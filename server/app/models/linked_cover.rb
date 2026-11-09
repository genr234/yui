# Cover art for imported games, stored like GameCover's at storage/covers/linked/ and served
# publicly at /covers/linked/<key> (kiosks and phones can't reach the private source).
# The key carries a content digest so a changed cover gets a new URL. A missing cover is
# never an error.
class LinkedCover
  KEY_FORMAT = /\A\d+-\h{8}\.(png|jpg|webp)\z/
  EXTENSIONS = { "image/png" => "png", "image/jpeg" => "jpg", "image/webp" => "webp" }.freeze
  TYPES = EXTENSIONS.invert.freeze

  def self.dir
    AssetStorage.covers_dir.join("linked")
  end

  def self.valid_key?(key)
    key.is_a?(String) && key.match?(KEY_FORMAT)
  end

  # Replaces any earlier cover for the asset; returns the public path, e.g. "/covers/linked/7-1a2b3c4d.png".
  def self.store(asset, cover)
    key = "#{asset.id}-#{Digest::SHA256.hexdigest(cover.bytes)[0, 8]}.#{EXTENSIONS.fetch(cover.content_type)}"
    FileUtils.mkdir_p(dir)
    remove_for(asset.id, except: key)
    temp = dir.join("#{key}.#{SecureRandom.hex(4)}.tmp")
    File.binwrite(temp, cover.bytes)
    File.rename(temp, dir.join(key))
    "/covers/linked/#{key}"
  end

  def self.remove_for(asset_id, except: nil)
    return unless dir.directory?

    dir.children.each do |child|
      name = child.basename.to_s
      child.delete if name.start_with?("#{asset_id}-") && name != except
    end
  end

  # The stored file for a key, taken from the directory listing, never built from the caller's string.
  def self.cached(key)
    return unless valid_key?(key) && dir.directory?

    dir.children.find { |child| child.basename.to_s == key && child.file? }
  end

  def self.content_type(key)
    TYPES.fetch(File.extname(key).delete_prefix("."))
  end
end
