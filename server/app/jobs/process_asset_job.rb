# Runs once an upload is complete: hashes the file, reads the disc header of games,
# fetches the cover, marks the asset ready and tells every kiosk on the account to
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
    describe_game(asset, path, base_url) if asset.game?
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

  def describe_game(asset, path, base_url)
    disc = DiscImage.read(path)
    metadata = asset.metadata.to_h.merge(
      "game_id" => disc.game_id,
      "platform" => disc.platform || (asset.kind == "wii-game" ? "wii" : "gamecube"),
      "region" => disc.region,
      "disc_title" => disc.disc_title,
      "format" => disc.format,
      "cover_url" => nil
    )

    if disc.detected?
      # The disc knows better than the form which console it belongs to.
      asset.kind = disc.platform == "wii" ? "wii-game" : "gamecube-game"
      asset.title = disc.disc_title if disc.disc_title.present? && asset.title == default_title(asset)
      metadata["cover_url"] = cover_url(disc, base_url)
    end

    asset.metadata = metadata
  end

  def cover_url(disc, base_url)
    path = GameCover.fetch(disc.game_id, disc.platform)
    return if path.nil? || base_url.blank?

    GameCover.url_for(disc.game_id, base_url)
  rescue StandardError => error
    # A missing cover never fails the asset.
    Rails.logger.warn("[ProcessAssetJob] cover for #{disc.game_id}: #{error.class}: #{error.message}")
    nil
  end

  def default_title(asset)
    Asset.default_title(asset.filename)
  end
end
