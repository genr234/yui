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

  test "shows the active session roster and 404s once it ends" do
    play_session = start_session(@kiosk)
    join_player(play_session)

    get "/api/kiosk/play_sessions/#{play_session.id}", headers: @auth, as: :json
    assert_response :ok
    assert_equal play_session.id.to_s, json["id"].to_s
    assert_equal [ "Ann" ], json["players"].map { |player| player["name"] }

    other = create_kiosk(token: "other-token")
    other_session = start_session(other)
    get "/api/kiosk/play_sessions/#{other_session.id}", headers: @auth, as: :json
    assert_response :not_found

    play_session.end!
    get "/api/kiosk/play_sessions/#{play_session.id}", headers: @auth, as: :json
    assert_response :not_found
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

  test "moves the speaker role" do
    play_session = start_session(@kiosk)
    host, = join_player(play_session, name: "Host")
    other, = join_player(play_session, name: "Other")

    get "/api/kiosk/play_sessions/#{play_session.id}", headers: @auth
    assert_equal [ true, false ], response.parsed_body["players"].map { |player| player["speaker"] }

    post "/api/kiosk/play_sessions/#{play_session.id}/players/#{other.id}/speaker", headers: @auth
    assert_response :no_content
    assert other.reload.speaker?
    assert_not host.reload.speaker?

    post "/api/kiosk/play_sessions/#{play_session.id}/players/0/speaker", headers: @auth
    assert_response :not_found
  end

  test "pairs a second speaker as a stereo channel" do
    play_session = start_session(@kiosk)
    host, = join_player(play_session, name: "Host")
    other, = join_player(play_session, name: "Other")

    post "/api/kiosk/play_sessions/#{play_session.id}/players/#{other.id}/speaker", headers: @auth, params: { channel: "right" }
    assert_response :no_content
    assert_equal "right", other.reload.speaker_channel
    assert_equal "left", host.reload.speaker_channel

    get "/api/kiosk/play_sessions/#{play_session.id}", headers: @auth
    assert_equal [ "left", "right" ], response.parsed_body["players"].map { |player| player["speaker_channel"] }

    post "/api/kiosk/play_sessions/#{play_session.id}/players/#{other.id}/speaker", headers: @auth, params: { channel: "middle" }
    assert_response :unprocessable_entity

    post "/api/kiosk/play_sessions/#{play_session.id}/players/#{other.id}/speaker", headers: @auth
    assert_response :no_content
    assert_nil other.reload.speaker_channel
    assert_not host.reload.speaker?
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

  test "pass_seat moves the slot to a connected spectator" do
    play_session = start_session(@kiosk)
    giver, = join_player(play_session, name: "Ana")
    3.times { |n| join_player(play_session, name: "Fill#{n}") }
    dee, = join_player(play_session, name: "Dee")
    dee.mark_connected!("d")

    post "/api/kiosk/play_sessions/#{play_session.id}/players/#{giver.id}/pass_seat",
      params: { to: dee.id, slot: 1 }, headers: @auth, as: :json
    assert_response :no_content
    assert_equal 1, dee.reload.slot
    assert_nil giver.reload.slot
    assert giver.host?

    # The giver no longer has a seat to give.
    post "/api/kiosk/play_sessions/#{play_session.id}/players/#{giver.id}/pass_seat",
      params: { to: dee.id, slot: 1 }, headers: @auth, as: :json
    assert_response :conflict
    assert_equal "giver_not_seated", json["error"]
  end

  test "pass_seat rejects a seated, unknown or disconnected recipient and is kiosk scoped" do
    play_session = start_session(@kiosk)
    giver, = join_player(play_session, name: "Ana")
    bo, = join_player(play_session, name: "Bo")
    4.times { |n| join_player(play_session, name: "Fill#{n}") }
    ghost = play_session.players.active.find_by(slot: nil)
    url = "/api/kiosk/play_sessions/#{play_session.id}/players/#{giver.id}/pass_seat"

    post url, params: { to: bo.id }, headers: @auth, as: :json
    assert_response :unprocessable_entity
    assert_equal "recipient_not_spectator", json["error"]
    post url, params: { to: ghost.id }, headers: @auth, as: :json
    assert_response :unprocessable_entity
    post url, params: { to: 0 }, headers: @auth, as: :json
    assert_response :unprocessable_entity
    assert_equal 1, giver.reload.slot

    other_session = start_session(create_kiosk(account: @kiosk.account))
    stranger, = join_player(other_session)
    post "/api/kiosk/play_sessions/#{other_session.id}/players/#{stranger.id}/pass_seat",
      params: { to: ghost.id }, headers: @auth, as: :json
    assert_response :not_found
  end
end
