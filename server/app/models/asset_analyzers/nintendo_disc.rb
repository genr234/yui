# Wii and GameCube disc images (ISO/GCM, WBFS, WIA/RVZ, GCZ, CISO): game ID, region,
# disc title and a GameTDB cover. Sets metadata.platform to "wii" or "gamecube".
module AssetAnalyzers
  module NintendoDisc
    def self.call(path, base_url)
      disc = DiscImage.read(path)
      return unless disc.detected?

      Result.new(
        title: disc.disc_title.presence,
        metadata: {
          "platform" => disc.platform,
          "format" => disc.format,
          "game_id" => disc.game_id,
          "region" => disc.region,
          "disc_title" => disc.disc_title,
          "cover_url" => cover_url(disc, base_url)
        }
      )
    end

    def self.cover_url(disc, base_url)
      path = GameCover.fetch(disc.game_id, disc.platform)
      return if path.nil? || base_url.blank?

      GameCover.url_for(disc.game_id, base_url)
    rescue StandardError => error
      # A missing cover never fails the asset.
      Rails.logger.warn("[AssetAnalyzers::NintendoDisc] cover for #{disc.game_id}: #{error.class}: #{error.message}")
      nil
    end
  end
end
