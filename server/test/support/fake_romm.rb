require "rack/mock"

# A small RomM stand-in speaking just the endpoints LinkedProviders::Romm uses. It is
# installed as HttpClient's transport, so no socket is opened; every body is generated
# bytes (never real ROMs or box art).
class FakeRomm
  PNG = ("\x89PNG\r\n\x1A\n".b + ("\0" * 32).b).freeze
  TOKEN = "fake-access-token".freeze
  API_TOKEN = "rmm_#{"a" * 64}".freeze

  attr_accessor :version, :username, :password, :failing_downloads, :redirect_to, :requests, :hide_cover

  def initialize
    @version = "4.9.2"
    @username = "kim"
    @password = "hunter2"
    @failing_downloads = []
    @requests = []
    @platforms = [
      { "id" => 1, "slug" => "snes", "name" => "Super Nintendo", "rom_count" => 2 },
      { "id" => 2, "slug" => "genesis-slash-megadrive", "name" => "Sega Genesis", "rom_count" => 1 }
    ]
    @roms = [
      rom(10, 1, "snes", "Tiny Quest", "tiny-quest.sfc", 64),
      rom(11, 1, "snes", "Folder Game", "folder-game", 100, multi: true),
      rom(12, 2, "genesis-slash-megadrive", "Blast Test", "blast-test.md", 48)
    ]
    @collections = [ { "id" => 5, "name" => "Favourites", "rom_ids" => [ 10, 12 ] } ]
  end

  def rom_bytes(id)
    ("ROM#{id}-".b * 64)[0, find_rom(id)["fs_size_bytes"]]
  end

  def calls
    requests.map { |request| "#{request.method.upcase} #{request.uri.path}" }
  end

  # HttpClient transport interface.
  def call(request, address)
    requests << request
    @last_address = address
    status, headers, body = route(request)
    yield LinkedProviders::HttpClient::Response.new(status, headers) { |&block| body.each(&block) }
  end

  attr_reader :last_address

  private

  def rom(id, platform_id, slug, name, fs_name, size, multi: false)
    {
      "id" => id, "platform_id" => platform_id, "platform_slug" => slug, "name" => name, "fs_name" => fs_name,
      "regions" => [ "USA" ], "fs_size_bytes" => size, "has_multiple_files" => multi,
      "files" => multi ? [ { "file_name" => "a.bin", "file_size_bytes" => 50 }, { "file_name" => "b.cue", "file_size_bytes" => 50 } ] : [ { "file_name" => fs_name, "file_size_bytes" => size } ],
      "path_cover_large" => "/assets/romm/resources/roms/#{platform_id}/#{id}/cover/big.png?ts=1", "path_cover_small" => nil
    }
  end

  def route(request)
    path = request.uri.path
    return redirect(request) if redirect_to && path.start_with?("/api/platforms")
    return json(200, { "SYSTEM" => { "VERSION" => version } }) if path == "/api/heartbeat"
    return token(request) if path == "/api/token"
    return json(401, { "detail" => "Not authenticated" }) unless authorized?(request)

    case path
    when "/api/platforms" then json(200, @platforms)
    when "/api/collections" then json(200, @collections)
    when "/api/roms" then roms_page(request)
    when %r{\A/api/roms/(\d+)/content/} then content(Regexp.last_match(1).to_i)
    when %r{\A/api/roms/(\d+)\z} then (found = find_rom(Regexp.last_match(1).to_i)) ? json(200, found) : json(404, {})
    when %r{\A/assets/romm/resources/roms/\d+/(\d+)/cover/big\.png\z}
      hide_cover ? json(404, {}) : [ 200, { "content-type" => "image/png" }, [ PNG ] ]
    else json(404, {})
    end
  end

  def find_rom(id)
    @roms.find { |row| row["id"] == id }
  end

  def authorized?(request)
    header = request.headers["Authorization"]
    [ "Bearer #{TOKEN}", "Bearer #{API_TOKEN}" ].include?(header)
  end

  def token(request)
    form = Rack::Utils.parse_query(request.body.to_s)
    if form["grant_type"] == "password" && form["username"] == username && form["password"] == password
      json(200, { "access_token" => TOKEN, "token_type" => "bearer" })
    else
      json(401, { "detail" => "Incorrect username or password" })
    end
  end

  def roms_page(request)
    query = Rack::Utils.parse_query(request.uri.query.to_s)
    rows = @roms
    rows = rows.select { |row| row["platform_id"].to_s == query["platform_ids"] } if query["platform_ids"]
    rows = rows.select { |row| row["name"].downcase.include?(query["search_term"].downcase) } if query["search_term"]
    if query["collection_id"]
      ids = @collections.find { |row| row["id"].to_s == query["collection_id"] }&.fetch("rom_ids").to_a
      rows = rows.select { |row| ids.include?(row["id"]) }
    end
    limit = query.fetch("limit", 50).to_i
    offset = query.fetch("offset", 0).to_i
    json(200, { "items" => rows.slice(offset, limit).to_a, "total" => rows.size, "limit" => limit, "offset" => offset })
  end

  def content(id)
    return json(500, {}) if failing_downloads.include?(id)

    [ 200, { "content-type" => "application/octet-stream" }, [ rom_bytes(id) ] ]
  end

  def redirect(_request)
    [ 302, { "location" => redirect_to }, [ "" ] ]
  end

  def json(status, data)
    [ status, { "content-type" => "application/json" }, [ JSON.generate(data) ] ]
  end
end

module FakeRommTestHelper
  PUBLIC_ADDRESS = "93.184.216.34".freeze

  def install_fake_romm
    @fake_romm = FakeRomm.new
    @saved_transport = LinkedProviders::HttpClient.transport
    @saved_resolver = LinkedProviders::HttpClient.resolver
    @saved_backoff = LinkedFetch.backoff
    LinkedProviders::HttpClient.transport = @fake_romm
    LinkedProviders::HttpClient.resolver = ->(_host) { [ PUBLIC_ADDRESS ] }
    LinkedFetch.backoff = []
    @fake_romm
  end

  def remove_fake_romm
    LinkedProviders::HttpClient.transport = @saved_transport
    LinkedProviders::HttpClient.resolver = @saved_resolver
    LinkedFetch.backoff = @saved_backoff
  end

  def link_romm(account, **attributes)
    account.linked_accounts.new(
      { provider: "romm", label: "Home RomM", base_url: "https://romm.example.test" }.merge(attributes)
    ).tap do |linked|
      linked.assign_credentials(auth: "password", username: @fake_romm.username, password: @fake_romm.password)
      linked.save!
    end
  end
end
