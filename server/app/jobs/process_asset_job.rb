# Runs once an upload is complete: hashes the file, runs the asset analyzers on games
# (see AssetAnalyzers), marks the asset ready and tells every kiosk on the account to
# reconcile. `base_url` (e.g. "https://yui.example.com") makes cover_url absolute.
class ProcessAssetJob < ApplicationJob
  queue_as :default

  READ_CHUNK = 4.megabytes

  def perform(asset_id, base_url = nil)
    asset = Asset.find_by(id: asset_id)
    return if asset.nil? || !asset.processing?

    path = asset.file_path
    raise "File is missing" unless path.file?

    size = path.size
    raise "File is #{size} bytes, expected #{asset.byte_size}" if size != asset.byte_size

    asset.sha256 = sha256_of(path)
    analyze_game(asset, path, base_url) if asset.game?
    asset.status = "ready"
    asset.error = nil
    asset.save!
    asset.request_kiosk_reconcile!
  rescue StandardError => error
    raise if asset.nil?

    Rails.logger.error("[ProcessAssetJob] asset #{asset.id}: #{error.class}: #{error.message}")
    asset.reload.update!(status: "failed", error: error.message.truncate(500)) if Asset.exists?(asset.id)
  end

  private

  def sha256_of(path)
    digest = Digest::SHA256.new
    buffer = +""
    File.open(path, "rb") do |file|
      digest.update(buffer) while file.read(READ_CHUNK, buffer)
    end
    digest.hexdigest
  end

  def analyze_game(asset, path, base_url)
    result = AssetAnalyzers.analyze_game(path, base_url)
    asset.metadata = asset.metadata.to_h.merge(AssetAnalyzers::GAME_DEFAULTS).merge(result&.metadata || {})
    # The uploader's own title wins; otherwise use what the analyzer found.
    asset.title = result.title if result&.title && asset.title == default_title(asset)
  end

  def default_title(asset)
    Asset.default_title(asset.filename)
  end
end
