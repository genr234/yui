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
end
