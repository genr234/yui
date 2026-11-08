require "net/http"

# Box art from GameTDB, cached at storage/covers/<GAME_ID>.png and served publicly at
# /covers/<GAME_ID>.png. A missing cover is never an error.
class GameCover
  GAME_ID_FORMAT = DiscImage::GAME_ID_FORMAT
  TIMEOUT = 10 # seconds, per request
  MAX_BYTES = 5.megabytes
  PNG_SIGNATURE = "\x89PNG\r\n\x1A\n".b

  # GameTDB cover languages to try, by the region character of the game ID.
  LANGUAGES = {
    "E" => %w[US], "N" => %w[US],
    "J" => %w[JA], "K" => %w[KO], "Q" => %w[KO], "T" => %w[KO], "W" => %w[ZH], "C" => %w[ZH],
    "P" => %w[EN], "X" => %w[EN], "Y" => %w[EN], "Z" => %w[EN], "L" => %w[EN], "M" => %w[EN],
    "D" => %w[DE EN], "F" => %w[FR EN], "I" => %w[IT EN], "S" => %w[ES EN], "H" => %w[NL EN],
    "V" => %w[SE EN], "U" => %w[AU EN], "R" => %w[RU EN]
  }.freeze
  FALLBACK_LANGUAGES = %w[US EN JA].freeze

  # Callable(URI) -> PNG bytes or nil. Replaced in tests so nothing hits the network.
  cattr_accessor :downloader, default: ->(uri) { GameCover.http_get(uri) }

  def self.valid_id?(game_id)
    game_id.is_a?(String) && game_id.match?(GAME_ID_FORMAT)
  end

  def self.path_for(game_id)
    AssetStorage.covers_dir.join("#{game_id}.png") if valid_id?(game_id)
  end

  # The cached cover file for a valid game ID, or nil. The path is taken from the
  # directory listing, never built from the caller's string.
  def self.cached(game_id)
    return unless valid_id?(game_id) && AssetStorage.covers_dir.directory?

    name = "#{game_id}.png"
    AssetStorage.covers_dir.children.find { |child| child.basename.to_s == name && child.file? }
  end

  def self.url_for(game_id, base_url)
    "#{base_url.to_s.chomp("/")}/covers/#{game_id}.png"
  end

  # GameTDB keeps GameCube covers under /wii/ too, so that path is tried for both.
  def self.candidate_urls(game_id, platform)
    languages = (LANGUAGES.fetch(game_id[3], []) + FALLBACK_LANGUAGES).uniq
    platforms = platform == "gamecube" ? %w[wii gc] : %w[wii]
    platforms.flat_map do |path|
      languages.map { |language| URI("https://art.gametdb.com/#{path}/cover/#{language}/#{game_id}.png") }
    end
  end

  # Returns the cached path, downloading it first if needed; nil when there is no cover.
  def self.fetch(game_id, platform)
    path = path_for(game_id)
    return if path.nil?
    return path if path.file?

    candidate_urls(game_id, platform).each do |uri|
      body = begin
        downloader.call(uri)
      rescue Timeout::Error, SocketError, SystemCallError, OpenSSL::SSL::SSLError => error
        # GameTDB is unreachable; don't wait out the timeout for every candidate.
        Rails.logger.info("[GameCover] #{uri}: #{error.class}: #{error.message}")
        return nil
      rescue StandardError => error
        Rails.logger.info("[GameCover] #{uri}: #{error.class}: #{error.message}")
        nil
      end
      next unless png?(body)

      FileUtils.mkdir_p(path.dirname)
      temp = path.sub_ext(".png.#{SecureRandom.hex(4)}.tmp")
      File.binwrite(temp, body)
      File.rename(temp, path)
      return path
    end
    nil
  end

  def self.png?(body)
    body.is_a?(String) && body.bytesize.between?(PNG_SIGNATURE.bytesize + 1, MAX_BYTES) &&
      body.byteslice(0, PNG_SIGNATURE.bytesize).b == PNG_SIGNATURE
  end

  def self.http_get(uri)
    Net::HTTP.start(uri.host, uri.port, use_ssl: uri.scheme == "https",
      open_timeout: TIMEOUT, read_timeout: TIMEOUT, write_timeout: TIMEOUT, ssl_timeout: TIMEOUT) do |http|
      response = http.request(Net::HTTP::Get.new(uri))
      return unless response.is_a?(Net::HTTPOK)
      return if response.content_length.to_i > MAX_BYTES

      response.body
    end
  end
end
