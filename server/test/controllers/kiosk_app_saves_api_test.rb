require "test_helper"

class KioskAppSavesApiTest < ActionDispatch::IntegrationTest
  include PlaySessionTestHelper

  APP = "dev.genr.arcade"
  BLOB = ("save-state".b * 50).freeze

  setup do
    PlayController.cache_store.clear
    @owner = Account.create!(name: "Lobby")
    @kiosk = create_kiosk(account: @owner, token: "kiosk-token")
    @auth = { "Authorization" => "Bearer kiosk-token" }
    @play_session = start_session(@kiosk, lobby_open: true)
    @player_account = Account.create!(name: "Riccardo")
    post "/play/#{@play_session.code}/join", params: { name: "Ann", color: "blue" }, as: :json
    @player = Player.find(JSON.parse(response.body).dig("player", "id"))
  end

  def json
    JSON.parse(response.body)
  end

  def path(key = nil, app: APP, player: nil)
    base = "/api/kiosk/app_saves/#{app}"
    base += "/#{key}" if key
    base += "?player_id=#{player.id}" if player
    base
  end

  def put_save(key, body = BLOB, headers: {}, **options)
    put path(key, **options), params: body, headers: @auth.merge("Content-Type" => "application/octet-stream").merge(headers)
  end

  def link_player(grant: true)
    link = PhoneLink.create!(account: @player_account, token_digest: SecureRandom.hex(8), label: "Phone")
    @player.update!(account: @player_account, phone_link: link)
    @player_account.profile_grants.create!(app_id: APP) if grant
  end

  test "requires a kiosk token" do
    get path
    assert_response :unauthorized
    put path("a"), params: BLOB
    assert_response :unauthorized
  end

  test "kiosk scope round-trips bytes with sha256 and updated_at" do
    put_save("slot-1", headers: { "X-Updated-At" => "2026-11-01T10:00:00Z" })
    assert_response :success
    assert_equal({ "key" => "slot-1", "size" => BLOB.bytesize, "sha256" => Digest::SHA256.hexdigest(BLOB), "updated_at" => "2026-11-01T10:00:00Z" }, json)

    get path("slot-1"), headers: @auth
    assert_response :success
    assert_equal BLOB, response.body.b
    assert_equal Digest::SHA256.hexdigest(BLOB), response.headers["X-Save-Sha256"]
    assert_equal "2026-11-01T10:00:00Z", response.headers["X-Updated-At"]

    get path, headers: @auth
    assert_equal [ { "key" => "slot-1", "size" => BLOB.bytesize, "sha256" => Digest::SHA256.hexdigest(BLOB), "updated_at" => "2026-11-01T10:00:00Z" } ], json["saves"]

    delete path("slot-1"), headers: @auth
    assert_response :no_content
    get path("slot-1"), headers: @auth
    assert_response :not_found
    assert_equal({ "error" => "not_found" }, json)
    delete path("slot-1"), headers: @auth
    assert_response :no_content
    assert_empty AppSave.all
  end

  test "an empty blob is allowed" do
    put_save("empty", "")
    assert_response :success
    assert_equal 0, json["size"]
  end

  test "lists are per app" do
    put_save("a")
    put_save("b", app: "other.app")
    get path, headers: @auth
    assert_equal [ "a" ], json["saves"].pluck("key")
  end

  test "invalid keys and app ids are 422" do
    [ "UPPER", "-lead", "has%20space", "a" * 81, "sl%C3%A4sh" ].each do |key|
      put_save(key)
      assert_response :unprocessable_content, key
      assert_equal "invalid_key", json["error"]
    end
    get path("Bad"), headers: @auth
    assert_response :unprocessable_content
    delete path("Bad"), headers: @auth
    assert_response :unprocessable_content
    get path(nil, app: "-nope"), headers: @auth
    assert_equal "invalid_app_id", json["error"]
    assert_response :unprocessable_content
    put_save("ok" + "a" * 77)
    assert_response :success
  end

  test "invalid X-Updated-At is 422" do
    put_save("a", headers: { "X-Updated-At" => "yesterday" })
    assert_response :unprocessable_content
    assert_equal "invalid_updated_at", json["error"]
  end

  test "future updated_at is clamped to now" do
    freeze_time do
      put_save("a", headers: { "X-Updated-At" => 1.year.from_now.iso8601 })
      assert_equal Time.current.utc.change(usec: 0).iso8601, json["updated_at"]
    end
  end

  test "a missing updated_at means now" do
    freeze_time do
      put_save("a")
      assert_equal Time.current.utc.change(usec: 0).iso8601, json["updated_at"]
    end
  end

  test "an older write is refused with newer_save_exists, an equal or newer one wins" do
    put_save("a", "new", headers: { "X-Updated-At" => "2026-11-02T00:00:00Z" })
    put_save("a", "old", headers: { "X-Updated-At" => "2026-11-01T00:00:00Z" })
    assert_response :conflict
    assert_equal({ "error" => "newer_save_exists", "updated_at" => "2026-11-02T00:00:00Z" }, json)
    get path("a"), headers: @auth
    assert_equal "new", response.body

    put_save("a", "same", headers: { "X-Updated-At" => "2026-11-02T00:00:00Z" })
    assert_response :success
    put_save("a", "newest", headers: { "X-Updated-At" => "2026-11-03T00:00:00Z" })
    assert_response :success
    assert_equal 1, AppSave.count
    get path("a"), headers: @auth
    assert_equal "newest", response.body
  end

  test "413 too_large by Content-Length and by streamed size" do
    stub_const(AppSave, :MAX_BYTES, 100) do
      put_save("big", "x" * 101)
      assert_response :content_too_large
      assert_equal({ "error" => "too_large", "max_bytes" => 100 }, json)
      assert_equal 0, AppSave.count

      put_save("ok", "x" * 100)
      assert_response :success
    end
  end

  test "the 64 key cap is per scope and app; overwriting an existing key is allowed" do
    AppSave::MAX_KEYS.times { |index| put_save("k#{index}", "x") }
    assert_response :success
    assert_equal 64, AppSave.count

    put_save("one-too-many")
    assert_response :conflict
    assert_equal "too_many", json["error"]

    put_save("k0", "again")
    assert_response :success
    put_save("k0", "other app", app: "other.app")
    assert_response :success
  end

  test "saves belong to the kiosk's account" do
    put_save("a")
    other = create_kiosk(token: "other-token")
    get path("a"), headers: { "Authorization" => "Bearer other-token" }
    assert_response :not_found
    assert other
  end

  test "player scope: 404 for an unknown player, another kiosk's player or a kicked one" do
    get path(nil, player: Player.new(id: 999_999)), headers: @auth
    assert_response :not_found

    other_kiosk = create_kiosk(token: "other-token")
    get path(nil, player: @player), headers: { "Authorization" => "Bearer other-token" }
    assert_response :not_found
    assert other_kiosk

    link_player
    @player.update!(kicked_at: Time.current)
    get path(nil, player: @player), headers: @auth
    assert_response :not_found
  end

  test "player scope: 404 once the session has ended" do
    link_player
    @play_session.end!
    get path(nil, player: @player), headers: @auth
    assert_response :not_found
  end

  test "player scope: 409 not_linked for a guest, on every verb" do
    get path(nil, player: @player), headers: @auth
    assert_response :conflict
    assert_equal "not_linked", json["error"]
    put_save("a", player: @player)
    assert_response :conflict
    get path("a", player: @player), headers: @auth
    assert_response :conflict
    delete path("a", player: @player), headers: @auth
    assert_response :conflict
  end

  test "player scope: 403 not_granted without the app grant" do
    link_player(grant: false)
    get path(nil, player: @player), headers: @auth
    assert_response :forbidden
    assert_equal "not_granted", json["error"]
    put_save("a", player: @player)
    assert_response :forbidden
    put_save("a", app: "other.app", player: @player)
    assert_response :forbidden
  end

  test "player scope round-trips, lands on the player's account, and stays apart from kiosk scope" do
    link_player
    put_save("slot", "mine", player: @player, headers: { "X-Updated-At" => "2026-11-01T00:00:00Z" })
    assert_response :success
    put_save("slot", "kiosk")
    assert_response :success

    assert_equal [ [ @player_account.id, "player" ], [ @owner.id, "kiosk" ] ].sort, AppSave.pluck(:account_id, :scope).sort

    get path("slot", player: @player), headers: @auth
    assert_equal "mine", response.body
    get path("slot"), headers: @auth
    assert_equal "kiosk", response.body

    get path(nil, player: @player), headers: @auth
    assert_equal [ "slot" ], json["saves"].pluck("key")

    delete path("slot", player: @player), headers: @auth
    assert_response :no_content
    get path("slot"), headers: @auth
    assert_equal "kiosk", response.body
  end

  test "player scope keeps its own 64 key cap and size limit" do
    link_player
    AppSave::MAX_KEYS.times { |index| put_save("k#{index}", "x", player: @player) }
    put_save("extra", player: @player)
    assert_response :conflict
    put_save("extra")
    assert_response :success
    stub_const(AppSave, :MAX_BYTES, 3) do
      put_save("k0", "toolong", player: @player)
      assert_response :content_too_large
    end
  end

  private

  def stub_const(mod, name, value)
    original = mod.const_get(name)
    mod.send(:remove_const, name)
    mod.const_set(name, value)
    yield
  ensure
    mod.send(:remove_const, name)
    mod.const_set(name, original)
  end
end
