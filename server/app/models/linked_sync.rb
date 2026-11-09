require "digest"

# Brings a linked account's chosen imports into the asset library (plan 16, L3 to L5):
# lists what every import covers, makes one `game` asset per ROM (bytes fetched once into
# Yui's storage, hashed on the way, then run through the normal analyzer chain), stores the
# cover, and removes assets that have left every import. One ROM failing never fails the
# sync; a login or connection problem stops it and is shown on the account.
class LinkedSync
  Result = Struct.new(:added, :updated, :removed, :failed, :errors) do
    def initialize(*) = super(0, 0, 0, 0, [])
  end

  # Seconds to wait between attempts at one ROM. Tests set this to [].
  cattr_accessor :backoff, default: [ 2, 10 ]

  RETRYABLE = [ LinkedProviders::ConnectionError ].freeze

  attr_reader :result

  def initialize(linked_account)
    @linked_account = linked_account
    @result = Result.new
  end

  def call
    return false unless @linked_account.begin_sync!

    @provider = @linked_account.provider_client
    clear_unfinished_assets
    wanted = enumerate
    existing = existing_assets
    wanted.each_value { |item| sync_rom(item, existing[item[:remote_id]]) }
    remove_unwanted(existing, wanted)
    # Games that failed one by one are reported, but the account itself is fine.
    @linked_account.finish_sync!(error: failure_summary, status: "ok")
    true
  rescue LinkedProviders::Error => error
    @linked_account.finish_sync!(error: error.message)
    false
  rescue StandardError => error
    Rails.logger.error("[LinkedSync] linked account #{@linked_account.id}: #{error.class}: #{error.message}")
    @linked_account.finish_sync!(error: "Sync stopped unexpectedly (#{error.class})")
    false
  end

  private

  # remote_id => item for everything the imports cover. Raises on anything that stops us
  # knowing the full list, so a half-read library never deletes games.
  def enumerate
    wanted = {}
    @linked_account.linked_imports.find_each do |import|
      case import.kind
      when "platform" then each_page(platform: import.remote_id) { |item| wanted[item[:remote_id]] = item }
      when "collection" then each_page(collection: import.remote_id) { |item| wanted[item[:remote_id]] = item }
      when "rom"
        begin
          item = @provider.item(import.remote_id)
          wanted[item[:remote_id]] = item
        rescue LinkedProviders::NotFoundError
          next
        end
      end
    end
    wanted
  end

  def each_page(**filter)
    page = 1
    loop do
      data = @provider.catalog(page: page, **filter)
      data[:items].each { |item| yield item }
      break if data[:items].empty? || page * data[:per_page] >= data[:total].to_i

      page += 1
    end
  end

  def existing_assets
    @linked_account.assets.index_by { |asset| asset.metadata.dig("source", "remote_id") }
  end

  def clear_unfinished_assets
    @linked_account.assets.where.not(status: "ready").find_each(&:destroy)
  end

  def sync_rom(item, current)
    if current && up_to_date?(current, item)
      refresh(current, item)
    else
      import_with_retries(item, current)
    end
  rescue StandardError => error
    record_failure(item, error)
  end

  def up_to_date?(asset, item)
    source = asset.metadata["source"].to_h
    asset.ready? && source["file"] == item[:filename] && source["remote_size"] == item[:size]
  end

  # Metadata that can change without new bytes (title, region), and a cover that was missing.
  def refresh(asset, item)
    fields = { "region" => item[:region] }
    fields["cover_url"] = fetch_cover(asset, item) if asset.metadata["cover_url"].blank?
    changed = fields.compact.any? { |key, value| asset.metadata[key] != value }
    return if !changed && asset.title == item[:title]

    asset.update!(title: item[:title], metadata: asset.metadata.merge(fields.compact))
    asset.request_kiosk_reconcile!
    @result.updated += 1
  end

  def import_with_retries(item, replacing)
    attempts = 0
    begin
      import(item, replacing)
    rescue *RETRYABLE => error
      delay = self.class.backoff[attempts]
      attempts += 1
      raise error if delay.nil?

      sleep(delay) if delay.positive?
      retry
    end
  end

  def import(item, replacing)
    asset = @linked_account.account.assets.create!(
      kind: "game", filename: item[:filename], title: item[:title].to_s.truncate(200), byte_size: 0,
      status: "uploading", linked_account: @linked_account, metadata: base_metadata(item)
    )
    begin
      store_file(asset, item)
      asset.update!(metadata: asset.metadata.merge("cover_url" => fetch_cover(asset, item)).compact)
      ProcessAssetJob.perform_now(asset.id, nil)
      asset.reload
      raise LinkedProviders::Error, asset.error.presence || "Yui couldn't process the file" unless asset.ready?
    rescue StandardError
      asset.destroy
      raise
    end
    replacing&.destroy
    replacing ? @result.updated += 1 : @result.added += 1
  end

  def base_metadata(item)
    {
      "source" => {
        "provider" => @linked_account.provider, "linked_account_id" => @linked_account.id,
        "remote_id" => item[:remote_id], "file" => item[:filename], "remote_size" => item[:size]
      },
      "platform" => item[:platform], "region" => item[:region]
    }.compact
  end

  # Streams the download to disk, hashing as it goes, within the upload size cap.
  def store_file(asset, item)
    asset.create_empty_file!
    digest = Digest::SHA256.new
    written = 0
    max = AssetUploadsController.max_size

    @provider.download(item[:remote_id]) do |source, _filename, size|
      File.open(asset.file_path, "wb") do |file|
        source.each_chunk do |chunk|
          written += chunk.bytesize
          raise LinkedProviders::Error, "The file is larger than the #{ActiveSupport::NumberHelper.number_to_human_size(max)} limit" if written > max

          digest.update(chunk)
          file.write(chunk)
        end
      end
      raise LinkedProviders::ConnectionError, "The download stopped early" if size && written != size
    end
    asset.update!(byte_size: written, sha256: digest.hexdigest, status: "processing")
  end

  # Returns the public cover path (or the one already stored); never raises.
  def fetch_cover(asset, item)
    cover = @provider.cover(item[:remote_id])
    cover ? LinkedCover.store(asset, cover) : asset.metadata["cover_url"]
  rescue StandardError => error
    Rails.logger.info("[LinkedSync] cover for #{item[:remote_id]}: #{error.class}: #{error.message}")
    asset.metadata["cover_url"]
  end

  def remove_unwanted(existing, wanted)
    existing.each do |remote_id, asset|
      next if wanted.key?(remote_id)

      asset.destroy
      @result.removed += 1
    end
  end

  def record_failure(item, error)
    Rails.logger.warn("[LinkedSync] #{item[:remote_id]} #{item[:title]}: #{error.class}: #{error.message}")
    @result.failed += 1
    @result.errors << "#{item[:title]}: #{error.message}"
  end

  def failure_summary
    return if @result.failed.zero?

    "#{ApplicationController.helpers.pluralize(@result.failed, "game")} couldn't be imported (#{@result.errors.first(3).join("; ")})".truncate(1000)
  end
end
