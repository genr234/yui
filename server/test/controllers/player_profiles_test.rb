require "test_helper"

# Plan 10: a phone plays as a Yui account and a game keeps a profile for it.
class PlayerProfilesTest < ActionDispatch::IntegrationTest
  include PlaySessionTestHelper
  include ActionCable::TestHelper

  APP = "dev.genr.fruit-ninja"

  setup do
    PlayController.cache_store.clear
    @owner = Account.create!(name: "Lobby")
    @kiosk = create_kiosk(account: @owner, token: "kiosk-token")
    @auth = { "Authorization" => "Bearer kiosk-token" }
    @play_session = start_session(@kiosk, lobby_open: true)
    @player_account = Account.create!(name: "Riccardo")
    post "/play/#{@play_session.code}/join", params: { name: "Ann", color: "blue" }, as: :json
    @player = Player.find(json.dig("player", "id"))
  end

  def json
    JSON.parse(response.body)
  end

  def phone_code(account = @player_account)
    PairingCode.create_for!(account, purpose: PairingCode::PHONE).last
  end

  def link_phone
    post "/play/#{@play_session.code}/account", params: { link_code: phone_code, label: "Ann's phone" }, as: :json
    assert_response :success
  end

  def profile_path(player = @player, app = APP)
    "/api/kiosk/play_sessions/#{@play_session.id}/players/#{player.id}/profiles/#{app}"
  end

  test "a phone code links the phone and the player plays as the account" do
    assert_broadcast_on(PlaySessionChannel.broadcasting_for(@play_session), { type: "player_updated", player: @player.as_player_json.merge(account_linked: true) }) do
      link_phone
    end
    assert_equal({ "account_name" => "Riccardo", "account_linked" => true }, json)
    assert_equal @player_account, @player.reload.account
    assert_equal "Ann's phone", @player.phone_link.label

    get "/play/#{@play_session.code}/info"
    assert_equal "Riccardo", json["account_name"]
    assert_equal true, json.dig("player", "account_linked")
  end

  test "kiosk pairing codes can't link a phone and phone codes can't pair a kiosk" do
    kiosk_code = PairingCode.create_for!(@player_account).last
    post "/play/#{@play_session.code}/account", params: { link_code: kiosk_code }, as: :json
    assert_response :not_found
    assert_nil @player.reload.account

    post "/api/kiosk/pair", params: { code: phone_code, device_uid: "x" }, as: :json
    assert_response :not_found
  end

  test "the phone chooses per session and can forget the link" do
    link_phone
    patch "/play/#{@play_session.code}/account", params: { use: false }, as: :json
    assert_equal false, json["account_linked"]
    assert_equal "Riccardo", json["account_name"]
    assert_nil @player.reload.account

    patch "/play/#{@play_session.code}/account", params: { use: true }, as: :json
    assert_equal true, json["account_linked"]

    delete "/play/#{@play_session.code}/account", as: :json
    assert_equal({ "account_name" => nil, "account_linked" => false }, json)
    assert_not PhoneLink.last.live?

    patch "/play/#{@play_session.code}/account", params: { use: true }, as: :json
    assert_response :conflict
  end

  test "joining a new session with use_account picks the link up again" do
    link_phone
    @play_session.end!
    next_session = start_session(@kiosk, lobby_open: true)
    post "/play/#{next_session.code}/join", params: { name: "Ann", color: "blue", use_account: true }, as: :json
    assert_response :success
    assert_equal true, json.dig("player", "account_linked")
  end

  test "a game reads and writes a profile only once the phone allowed it" do
    get profile_path, headers: @auth
    assert_response :conflict
    assert_equal "not_linked", json["error"]

    link_phone
    get profile_path, headers: @auth
    assert_response :forbidden
    assert_equal "not_granted", json["error"]

    post "/play/#{@play_session.code}/profile_grants", params: { app_id: APP }, as: :json
    assert_response :no_content

    get profile_path, headers: @auth
    assert_response :success
    assert_nil json["data"]
    assert_equal 0, json["version"]
    profile_id = json["profile_id"]
    assert_match(/\A\h{24}\z/, profile_id)
    assert_not_includes response.body, "Riccardo"

    put profile_path, params: { data: { best: 42, unlocked: [ "zen" ] }, version: 0 }, headers: @auth, as: :json
    assert_response :success
    assert_equal 1, json["version"]

    put profile_path, params: { data: { best: 1 }, version: 0 }, headers: @auth, as: :json
    assert_response :conflict
    assert_equal({ "error" => "version_conflict", "version" => 1 }, json)

    get profile_path, headers: @auth
    assert_equal({ "best" => 42, "unlocked" => [ "zen" ] }, json["data"])
    assert_equal profile_id, json["profile_id"]

    # Another game gets its own grant check and its own opaque id.
    post "/play/#{@play_session.code}/profile_grants", params: { app_id: "dev.genr.subway" }, as: :json
    get profile_path(@player, "dev.genr.subway"), headers: @auth
    assert_not_equal profile_id, json["profile_id"]
  end

  test "revoking the grant or the link cuts the kiosk off" do
    link_phone
    post "/play/#{@play_session.code}/profile_grants", params: { app_id: APP }, as: :json
    delete "/play/#{@play_session.code}/profile_grants/#{APP}", as: :json
    get profile_path, headers: @auth
    assert_response :forbidden

    post "/play/#{@play_session.code}/profile_grants", params: { app_id: APP }, as: :json
    PhoneLink.last.revoke!
    get profile_path, headers: @auth
    assert_response :conflict
    assert_nil @player.reload.account
  end

  test "profiles are capped and scoped to the kiosk's own session" do
    link_phone
    post "/play/#{@play_session.code}/profile_grants", params: { app_id: APP }, as: :json
    put profile_path, params: { data: { blob: "x" * (PlayerProfile::MAX_BYTES + 1) }, version: 0 }, headers: @auth, as: :json
    assert_response :content_too_large

    create_kiosk(token: "other-token")
    get profile_path, headers: { "Authorization" => "Bearer other-token" }
    assert_response :not_found
  end

  test "the kiosk makes phone codes for its own account" do
    post "/api/kiosk/phone_link_codes", headers: @auth, as: :json
    assert_response :created
    assert_match(/\A\d{6}\z/, json["code"])

    post "/play/#{@play_session.code}/account", params: { link_code: json["code"] }, as: :json
    assert_equal "Lobby", json["account_name"]
  end

  test "account endpoints need a joined phone and a JSON same-origin request" do
    reset!
    post "/play/#{@play_session.code}/account", params: { link_code: phone_code }, as: :json
    assert_response :unauthorized

    post "/play/#{@play_session.code}/account", params: { link_code: phone_code }
    assert_response :unsupported_media_type
  end
end
