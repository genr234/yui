require "digest"

# Fetches one imported game's bytes from its linked account when a kiosk asks for it
# (LinkedSync only lists games). The asset goes remote -> fetching -> processing -> ready
# through the normal analyzer chain, which then tells kiosks to reconcile. On failure the
# asset goes back to remote with the reason in `error`, so it can be asked for again.
class LinkedFetch
  # Seconds to wait between attempts. Tests set this to [].
  cattr_accessor :backoff, default: [ 2, 10 ]

  RETRYABLE = [ LinkedProviders::ConnectionError ].freeze

  # Marks a remote asset as fetching and queues the job. Returns the asset's status.
  def self.request(asset)
    return asset.status unless asset.remote? && asset.linked_account

    claimed = Asset.where(id: asset.id, status: "remote").update_all(status: "fetching", error: nil, updated_at: Time.current)
    LinkedFetchJob.perform_later(asset.id) if claimed == 1
    asset.reload.status
  end

  def initialize(asset)
    @asset = asset
  end

  def call
    return false unless @asset.fetching? && @asset.linked_account

    provider = @asset.linked_account.provider_client
    with_retries { store_file(provider) }
    ProcessAssetJob.perform_now(@asset.id, nil)
    @asset.reload
    raise LinkedProviders::Error, @asset.error.presence || "Yui couldn't process the file" unless @asset.ready?

    true
  rescue StandardError => error
    Rails.logger.warn("[LinkedFetch] asset #{@asset.id}: #{error.class}: #{error.message}")
    FileUtils.rm_rf(@asset.directory)
    message = error.is_a?(LinkedProviders::Error) ? error.message : "The download stopped unexpectedly (#{error.class})"
    @asset.update_columns(status: "remote", sha256: nil, error: message.truncate(500), updated_at: Time.current) if Asset.exists?(@asset.id)
    false
  end

  private

  def with_retries
    attempts = 0
    begin
      yield
    rescue *RETRYABLE => error
      delay = self.class.backoff[attempts]
      attempts += 1
      raise error if delay.nil?

      sleep(delay) if delay.positive?
      retry
    end
  end

  # Streams the download to disk, hashing as it goes, within the upload size cap.
  def store_file(provider)
    remote_id = @asset.metadata.dig("source", "remote_id")
    @asset.create_empty_file!
    digest = Digest::SHA256.new
    written = 0
    max = AssetUploadsController.max_size

    provider.download(remote_id) do |source, _filename, size|
      digest = Digest::SHA256.new
      written = 0
      File.open(@asset.file_path, "wb") do |file|
        source.each_chunk do |chunk|
          written += chunk.bytesize
          raise LinkedProviders::Error, "The file is larger than the #{ActiveSupport::NumberHelper.number_to_human_size(max)} limit" if written > max

          digest.update(chunk)
          file.write(chunk)
        end
      end
      raise LinkedProviders::ConnectionError, "The download stopped early" if size && written != size
    end
    @asset.update!(byte_size: written, sha256: digest.hexdigest, status: "processing")
  end
end
