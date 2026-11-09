require "test_helper"

class LinkedProviders::RommTest < ActiveSupport::TestCase
  include FakeRommTestHelper

  setup do
    @fake = install_fake_romm
    @account = Account.create!(name: "Lobby")
    @linked = link_romm(@account)
    @romm = @linked.provider_client
  end

  teardown { remove_fake_romm }

  test "verify! reports the version and rom count" do
    info = @romm.verify!
    assert_equal "4.9.2", info[:version]
    assert_equal 3, info[:rom_count]
    assert_equal 2, info[:platform_count]
  end

  test "verify! with an api token sends it as a bearer token and never calls /token" do
    token_account = link_romm(@account, label: "Token RomM")
    token_account.assign_credentials(auth: "token", token: FakeRomm::API_TOKEN)
    token_account.save!
    @fake.requests.clear

    assert_equal 3, token_account.provider_client.verify![:rom_count]
    assert_not_includes @fake.calls, "POST /api/token"
  end

  test "verify! with a wrong password is an auth error in plain words" do
    @linked.assign_credentials(auth: "password", username: "kim", password: "wrong")
    error = assert_raises(LinkedProviders::AuthError) { @linked.provider_client.verify! }
    assert_equal "RomM rejected the login", error.message
  end

  test "verify! with a bad api token is an auth error" do
    @linked.assign_credentials(auth: "token", token: "rmm_nope")
    assert_raises(LinkedProviders::AuthError) { @linked.provider_client.verify! }
  end

  test "verify! on an unsupported version says the API changed" do
    @fake.version = "7.0.1"
    error = assert_raises(LinkedProviders::ApiChangedError) { @romm.verify! }
    assert_equal "RomM's API changed (version 7.0.1); Yui supports 4.x to 5.x", error.message
  end

  test "verify! on a server that is not RomM" do
    LinkedProviders::HttpClient.transport = lambda do |_request, _address, &block|
      block.call(LinkedProviders::HttpClient::Response.new(200, {}) { |&chunk| chunk.call("<html>hi</html>") })
    end
    assert_raises(LinkedProviders::ConnectionError) { @romm.verify! }
  end

  test "platforms map to Yui's slugs and collections list sizes" do
    platforms = @romm.platforms
    assert_equal [ "snes", "genesis" ], platforms.map { |platform| platform[:slug] }
    assert_equal [ { remote_id: "5", label: "Favourites", count: 2 } ], @romm.collections
    assert_equal "gamecube", LinkedProviders::Romm.map_platform("ngc")
    assert_equal "dc", LinkedProviders::Romm.map_platform("dc")
    assert_equal "psx", LinkedProviders::Romm.map_platform("psx")
  end

  test "catalog pages and searches" do
    page = @romm.catalog(page: 1, per_page: 2)
    assert_equal 3, page[:total]
    assert_equal [ "Tiny Quest", "Folder Game" ], page[:items].map { |item| item[:title] }
    assert_equal [ "Blast Test" ], @romm.catalog(page: 2, per_page: 2)[:items].map { |item| item[:title] }
    assert_equal [ "Blast Test" ], @romm.catalog(query: "blast")[:items].map { |item| item[:title] }
    assert_equal 2, @romm.catalog(platform: "1")[:total]
    assert_equal 2, @romm.catalog(collection: "5")[:total]
  end

  test "catalog items carry platform, region, filename and cover path" do
    item = @romm.item("10")
    assert_equal "snes", item[:platform]
    assert_equal "USA", item[:region]
    assert_equal "tiny-quest.sfc", item[:filename]
    assert_match %r{/cover/big\.png}, item[:cover_path]
    assert_equal "folder-game.zip", @romm.item("11")[:filename]
  end

  test "download streams the bytes" do
    received = +"".b
    @romm.download("10") do |source, filename, size|
      assert_equal "tiny-quest.sfc", filename
      assert_equal 64, size
      source.each_chunk { |chunk| received << chunk }
    end
    assert_equal @fake.rom_bytes(10), received
  end

  test "cover returns an image and nil when RomM has none" do
    cover = @romm.cover("10")
    assert_equal "image/png", cover.content_type
    assert_equal FakeRomm::PNG, cover.bytes

    @fake.hide_cover = true
    assert_nil @romm.cover("10")
  end

  test "an unexpected response shape is an api-changed error" do
    LinkedProviders::HttpClient.transport = lambda do |request, _address, &block|
      body = request.uri.path == "/api/heartbeat" ? { "SYSTEM" => { "VERSION" => "4.9.0" } } : { "unexpected" => true }
      block.call(LinkedProviders::HttpClient::Response.new(200, {}) { |&chunk| chunk.call(JSON.generate(body)) })
    end
    @linked.assign_credentials(auth: "token", token: "rmm_x")
    assert_raises(LinkedProviders::ApiChangedError) { @linked.provider_client.catalog }
  end
end
