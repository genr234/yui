require "test_helper"

class KioskPlaySessionsApiTest < ActionDispatch::IntegrationTest
  include PlaySessionTestHelper
  include ActionCable::TestHelper

  setup do
    @kiosk = create_kiosk(token: "kiosk-token")
    @auth = { "Authorization" => "Bearer kiosk-token" }
  end

  def json
    JSON.parse(response.body)
  end

  test "requires a kiosk token" do
    post "/api/kiosk/play_sessions", as: :json
    assert_response :unauthorized

    post "/api/kiosk/play_sessions", headers: { "Authorization" => "Bearer nope" }, as: :json
    assert_response :unauthorized
  end

  test "creates a session, then returns the active one" do
    post "/api/kiosk/play_sessions", headers: @auth, as: :json
    assert_response :created
    created = json
    assert_equal %w[code id join_base_url lobby_open players qr_key], created.keys.sort
    assert_kind_of String, created["id"]
    assert_equal "http://www.example.com/play/#{created["code"]}", created["join_base_url"]
    assert_equal false, created["lobby_open"]
    assert_equal [], created["players"]

    play_session = PlaySession.find(created["id"])
    join_player(play_session)

    post "/api/kiosk/play_sessions", headers: @auth, as: :json
    assert_response :ok
    assert_equal created["id"], json["id"]
    assert_equal [ "Ann" ], json["players"].map { |player| player["name"] }
  end

  test "updates lobby_open and ends the session" do
    play_session = start_session(@kiosk)
    player, = join_player(play_session)

    patch "/api/kiosk/play_sessions/#{play_session.id}", params: { lobby_open: true }, headers: @auth, as: :json
    assert_response :success
    assert_equal true, json["lobby_open"]
    assert play_session.reload.lobby_open

    assert_broadcast_on(PlaySessionChannel.broadcasting_for(player), { type: "session_ended" }) do
      delete "/api/kiosk/play_sessions/#{play_session.id}", headers: @auth
    end
    assert_response :no_content
    assert_not play_session.reload.active?

    patch "/api/kiosk/play_sessions/#{play_session.id}", params: { lobby_open: false }, headers: @auth, as: :json
    assert_response :not_found
  end

  test "kicks players and transfers host" do
    play_session = start_session(@kiosk)
    host, = join_player(play_session, name: "Host")
    other, = join_player(play_session, name: "Other")

    post "/api/kiosk/play_sessions/#{play_session.id}/players/#{other.id}/host", headers: @auth
    assert_response :no_content
    assert other.reload.host?
    assert_not host.reload.host?

    assert_enqueued_with(job: DisconnectPlayerJob, args: [ host ]) do
      post "/api/kiosk/play_sessions/#{play_session.id}/players/#{host.id}/kick", headers: @auth
    end
    assert_response :no_content
    assert host.reload.kicked?

    post "/api/kiosk/play_sessions/#{play_session.id}/players/#{host.id}/kick", headers: @auth
    assert_response :not_found
  end

  test "is scoped to the kiosk's own sessions" do
    other_session = start_session(create_kiosk(account: @kiosk.account))
    player, = join_player(other_session)

    patch "/api/kiosk/play_sessions/#{other_session.id}", params: { lobby_open: true }, headers: @auth, as: :json
    assert_response :not_found
    delete "/api/kiosk/play_sessions/#{other_session.id}", headers: @auth
    assert_response :not_found
    post "/api/kiosk/play_sessions/#{other_session.id}/players/#{player.id}/kick", headers: @auth
    assert_response :not_found
    get "/api/kiosk/play_sessions/#{other_session.id}/ice", headers: @auth
    assert_response :not_found

    own_session = start_session(@kiosk)
    post "/api/kiosk/play_sessions/#{own_session.id}/players/#{player.id}/kick", headers: @auth
    assert_response :not_found

    assert other_session.reload.active?
    assert_not player.reload.kicked?
    assert_not other_session.lobby_open
  end

  test "returns ICE config for the kiosk" do
    play_session = start_session(@kiosk)

    get "/api/kiosk/play_sessions/#{play_session.id}/ice", headers: @auth
    assert_response :success
    assert json["ice_servers"].first["urls"].any?
    assert json["ttl"].positive?
  end
end
