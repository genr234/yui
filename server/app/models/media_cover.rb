# Cover images for assets that have no GameTDB cover: art embedded in audio files and
# images an admin attached to a game. Files are content-addressed
# (storage/media_covers/<32 hex of sha256>.<ext>) and served publicly at
# /media_covers/<key>, like GameCover's.
class MediaCover
  MAX_BYTES = 5.megabytes
  KEY_FORMAT = /\A\h{32}\.(png|jpg|gif|webp)\z/

  # [magic prefix, extension]
  SIGNATURES = [
    [ "\x89PNG\r\n\x1A\n".b, "png" ],
    [ "\xFF\xD8\xFF".b, "jpg" ],
    [ "GIF8".b, "gif" ]
  ].freeze

  def self.extension_for(bytes)
    return unless bytes.is_a?(String) && bytes.bytesize > 12

    head = bytes.byteslice(0, 12).b
    return "webp" if head.start_with?("RIFF".b) && head.byteslice(8, 4) == "WEBP".b

    SIGNATURES.find { |magic, _| head.start_with?(magic) }&.last
  end

  def self.valid_key?(key)
    key.is_a?(String) && KEY_FORMAT.match?(key)
  end

  def self.path_for(key)
    AssetStorage.media_covers_dir.join(key) if valid_key?(key)
  end

  # Like GameCover.cached: the path comes from the directory listing, never the caller's string.
  def self.cached(key)
    return unless valid_key?(key) && AssetStorage.media_covers_dir.directory?

    AssetStorage.media_covers_dir.children.find { |child| child.basename.to_s == key && child.file? }
  end

  def self.url_for(key, base_url)
    "#{base_url.to_s.chomp("/")}/media_covers/#{key}"
  end

  # Saves image bytes and returns the key, or nil when they are not a supported image.
  def self.store(bytes)
    return if bytes.nil? || bytes.bytesize > MAX_BYTES

    extension = extension_for(bytes)
    return if extension.nil?

    key = "#{Digest::SHA256.hexdigest(bytes).first(32)}.#{extension}"
    path = path_for(key)
    return key if path.file?

    FileUtils.mkdir_p(path.dirname)
    temp = path.sub_ext(".#{SecureRandom.hex(4)}.tmp")
    File.binwrite(temp, bytes)
    File.rename(temp, path)
    key
  end

  def self.store_file(path)
    return unless File.file?(path) && File.size(path) <= MAX_BYTES

    store(File.binread(path))
  end
end
