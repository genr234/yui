# RomM provider (https://github.com/rommapp/romm).
#
# Coded against the RomM 4.x - 5.0 REST API (everything under /api), read from RomM's public
# source (backend/endpoints) and docs (docs.romm.app 4.9 / 5.0). Not yet exercised against a
# live instance; see docs/plans/handoff/16-linked-accounts-romm.md for what is unverified.
# Versions outside SUPPORTED_MAJORS fail with "RomM's API changed" instead of guessing.
#
# Endpoints used:
#   GET  /api/heartbeat                        version (SYSTEM.VERSION); no auth
#   POST /api/token                            OAuth2 password grant (form) -> bearer access token
#   GET  /api/platforms                        [{ id, slug, name, rom_count }]
#   GET  /api/collections                      [{ id, name, rom_ids }]
#   GET  /api/roms                             { items, total, limit, offset }; platform_ids,
#                                              collection_id, search_term, with_files
#   GET  /api/roms/{id}                        one ROM
#   GET  /api/roms/{id}/content/{file_name}    the file (a zip for multi-file ROMs)
#   GET  <path_cover_large>                    cover image (RomM's /assets/... resources path)
#
# Auth: a client API token (`rmm_...`, sent as a bearer token) or username + password
# (exchanged at /api/token for a bearer token).
module LinkedProviders
  class Romm
    SUPPORTED_MAJORS = (4..5)
    SUPPORTED_LABEL = "4.x to 5.x".freeze
    PAGE_SIZE = 50
    SCOPES = "roms.read platforms.read collections.read".freeze
    MAX_COVER_BYTES = 5.megabytes
    COVER_TYPES = {
      "\x89PNG\r\n\x1A\n".b => "image/png",
      "\xFF\xD8\xFF".b => "image/jpeg"
    }.freeze

    # RomM platform slugs (IGDB-style) -> Yui's platform set. Others are stored as-is.
    PLATFORM_SLUGS = {
      "nes" => "nes", "famicom" => "nes",
      "snes" => "snes", "sfam" => "snes", "super-famicom" => "snes",
      "gb" => "gb", "gbc" => "gbc", "gba" => "gba",
      "genesis" => "genesis", "genesis-slash-megadrive" => "genesis", "megadrive" => "genesis", "sega-mega-drive-slash-genesis" => "genesis",
      "sms" => "sms", "sega-master-system" => "sms", "sega-master-system-slash-mark-iii" => "sms",
      "gg" => "gg", "gamegear" => "gg", "game-gear" => "gg", "sega-game-gear" => "gg",
      "pce" => "pce", "tg16" => "pce", "turbografx16--1" => "pce", "turbografx-16-slash-pc-engine" => "pce", "pc-engine" => "pce",
      "n64" => "n64", "atari2600" => "atari2600", "lynx" => "lynx", "atari-lynx" => "lynx",
      "wii" => "wii", "ngc" => "gamecube", "gc" => "gamecube", "gamecube" => "gamecube"
    }.freeze

    def self.map_platform(slug)
      PLATFORM_SLUGS.fetch(slug.to_s.downcase, slug.presence)
    end

    def initialize(linked_account)
      @account = linked_account
      @credentials = linked_account.credential_data
      @http = HttpClient.new(linked_account.base_url, allow_local_network: linked_account.allow_local_network)
    end

    def verify!
      version = server_version!
      platforms = platform_list
      { version: version, rom_count: platforms.sum { |platform| platform[:count] }, platform_count: platforms.size }
    end

    def platforms
      platform_list
    end

    def collections
      list = api_json("/collections")
      raise ApiChangedError, api_changed_message unless list.is_a?(Array)

      list.map do |row|
        { remote_id: row.fetch("id").to_s, label: row["name"].to_s, count: Array(row["rom_ids"]).size }
      end
    rescue KeyError
      raise ApiChangedError, api_changed_message
    end

    def catalog(page: 1, query: nil, platform: nil, collection: nil, per_page: PAGE_SIZE)
      page = [ page.to_i, 1 ].max
      data = api_json("/roms", query: {
        limit: per_page, offset: (page - 1) * per_page, with_files: true,
        with_char_index: false, with_filter_values: false, with_rom_id_index: false,
        search_term: query.presence, platform_ids: platform.presence, collection_id: collection.presence,
        order_by: "name", order_dir: "asc"
      })
      raise ApiChangedError, api_changed_message unless data.is_a?(Hash) && data["items"].is_a?(Array)

      items = data["items"].map { |rom| normalize(rom) }
      { items: items, total: data["total"] || items.size, page: page, per_page: per_page }
    end

    def item(remote_id)
      normalize(api_json("/roms/#{Integer(remote_id)}"))
    end

    # Streams the ROM (RomM zips multi-file ROMs itself) as `yield(source, filename, size)`.
    def download(remote_id)
      rom = item(remote_id)
      path = "/roms/#{rom[:remote_id]}/content/#{ERB::Util.url_encode(rom[:filename])}"
      with_auth_retry do
        @http.request(:get, "/api#{path}", headers: auth_headers) do |response|
          check_status!(response)
          size = response.headers["content-length"]&.to_i || (rom[:multi] ? nil : rom[:size])
          yield response, rom[:filename], size
        end
      end
    end

    def cover(remote_id)
      rom = item(remote_id)
      path = rom[:cover_path]
      return if path.blank? || !path.start_with?("/") || path.start_with?("//") || path.include?("..")

      bytes = with_auth_retry do
        @http.request(:get, path.split("?").first, query: Rack::Utils.parse_query(path.split("?", 2)[1].to_s),
          headers: auth_headers) do |response|
          return nil unless response.success?

          response.body(limit: MAX_COVER_BYTES)
        end
      end
      content_type = image_type(bytes)
      content_type && Cover.new(bytes: bytes, content_type: content_type)
    rescue NotFoundError
      nil
    end

    private

    def image_type(bytes)
      return "image/webp" if bytes.byteslice(0, 4) == "RIFF" && bytes.byteslice(8, 4) == "WEBP"

      COVER_TYPES.find { |signature, _| bytes.byteslice(0, signature.bytesize)&.b == signature }&.last
    end

    def platform_list
      list = api_json("/platforms")
      raise ApiChangedError, api_changed_message unless list.is_a?(Array)

      list.map do |row|
        {
          remote_id: row.fetch("id").to_s, slug: Romm.map_platform(row["slug"]), remote_slug: row["slug"].to_s,
          label: row["display_name"].presence || row["name"].to_s, count: row["rom_count"].to_i
        }
      end
    rescue KeyError
      raise ApiChangedError, api_changed_message
    end

    def normalize(rom)
      raise ApiChangedError, api_changed_message unless rom.is_a?(Hash) && rom["id"] && rom["fs_name"]

      files = Array(rom["files"])
      multi = rom["has_multiple_files"] == true || files.size > 1
      fs_name = rom["fs_name"].to_s
      {
        remote_id: rom["id"].to_s,
        title: rom["name"].presence || File.basename(fs_name, ".*"),
        platform: Romm.map_platform(rom["platform_slug"]),
        region: Array(rom["regions"]).first.presence,
        filename: multi ? "#{fs_name}.zip" : fs_name,
        size: rom["fs_size_bytes"] || files.sum { |file| file["file_size_bytes"].to_i },
        multi: multi,
        cover_path: rom["path_cover_large"].presence || rom["path_cover_small"].presence
      }
    end

    # Checks the heartbeat, which also proves the URL is a RomM and which version it is.
    def server_version!
      response = @http.request(:get, "/api/heartbeat", headers: { "Accept" => "application/json" })
      data = parse_json(response.body, not_romm: true) if response.success?
      version = data.is_a?(Hash) ? data.dig("SYSTEM", "VERSION") : nil
      raise ConnectionError, "That address doesn't look like a RomM server" if version.blank?

      major = version.to_s[/\A\d+/]&.to_i
      if major && !SUPPORTED_MAJORS.cover?(major)
        raise ApiChangedError, "RomM's API changed (version #{version}); Yui supports #{SUPPORTED_LABEL}"
      end

      @version = version.to_s
    end

    def api_json(path, query: {})
      with_auth_retry do
        @http.request(:get, "/api#{path}", query: query, headers: auth_headers.merge("Accept" => "application/json")) do |response|
          check_status!(response)
          parse_json(response.body)
        end
      end
    end

    def check_status!(response)
      return if response.success?

      case response.status
      when 401, 403 then raise AuthError, "RomM rejected the login"
      when 404 then raise NotFoundError, "RomM doesn't have that"
      else raise ConnectionError, "RomM answered with an error (HTTP #{response.status})"
      end
    end

    def parse_json(body, not_romm: false)
      JSON.parse(body)
    rescue JSON::ParserError
      raise ConnectionError, "That address doesn't look like a RomM server" if not_romm

      raise ApiChangedError, api_changed_message
    end

    def api_changed_message
      "RomM's API changed (version #{@version || "unknown"}); Yui supports #{SUPPORTED_LABEL}"
    end

    # A password login gets a bearer token that can expire; log in again once on a 401.
    def with_auth_retry
      attempts = 0
      begin
        yield
      rescue AuthError
        raise unless @credentials["auth"] == "password" && @token && (attempts += 1) == 1

        @token = nil
        retry
      end
    end

    def auth_headers
      { "Authorization" => "Bearer #{bearer_token}" }
    end

    def bearer_token
      return @credentials["token"].to_s if @credentials["auth"] == "token"

      @token ||= fetch_token
    end

    def fetch_token
      response = @http.request(:post, "/api/token", headers: { "Accept" => "application/json" }, form: {
        grant_type: "password", username: @credentials["username"].to_s, password: @credentials["password"].to_s,
        scope: SCOPES
      })
      raise AuthError, "RomM rejected the login" if [ 400, 401, 403 ].include?(response.status)
      raise ConnectionError, "RomM answered with an error (HTTP #{response.status})" unless response.success?

      token = parse_json(response.body).then { |data| data.is_a?(Hash) ? data["access_token"] : nil }
      raise ApiChangedError, api_changed_message if token.blank?

      token
    end
  end
end
