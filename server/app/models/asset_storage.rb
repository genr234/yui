# Disk layout for the asset library. Everything lives under `storage/`, which is the
# `server_storage` Kamal volume in production:
#
#   storage/assets/<account_id>/<asset_id>/<filename>   uploaded files (written in place by tus)
#   storage/covers/<GAME_ID>.png                        cached GameTDB covers
module AssetStorage
  mattr_accessor :root, default: Rails.root.join("storage")

  def self.assets_dir
    Pathname(root).join("assets")
  end

  def self.covers_dir
    Pathname(root).join("covers")
  end

  # storage/media_covers/<key>.<ext>: art embedded in audio files, and images an admin
  # attached to a game.
  def self.media_covers_dir
    Pathname(root).join("media_covers")
  end

  # storage/app_saves/<account_id>/<save id>: app save blobs.
  def self.app_saves_dir
    Pathname(root).join("app_saves")
  end
end
