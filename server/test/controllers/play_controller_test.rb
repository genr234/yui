require "test_helper"

class PlayControllerTest < ActionDispatch::IntegrationTest
  include PlaySessionTestHelper
  include ActionCable::TestHelper

  setup do
    PlayController.cache_store.clear
    @play_session = start_session
  end

  def join(headers: {}, **params)
    post "/play/#{@play_session.code}/join",
      params: { name: "Ann", color: "blue" }.merge(params),
      headers: headers,
      as: :json
  end

  def json
    JSON.parse(response.body)
  end

  test "GET /play/:code serves a placeholder without login when the pad isn't built" do
    User.create!(name: "Admin", webauthn_id: "admin") # login is required for the control room

    with_pad_index(Rails.root.join("tmp", "missing-pad", "index.html")) do
      get "/play/#{@play_session.code.downcase}"
    end

    assert_response :success
    assert_includes response.body, "hasn't been built yet"
    assert_includes response.body, @play_session.code
    assert_not_includes response.body, "stylesheet" # no app layout
  end

  test "GET /play/:code serves the built pad index" do
    dir = Rails.root.join("tmp", "pad-test-#{SecureRandom.hex(4)}")
    FileUtils.mkdir_p(dir)
    File.write(dir.join("index.html"), "<!doctype html><title>Pad</title><div id=app></div>")

    with_pad_index(dir.join("index.html")) do
      get "/play/#{@play_session.code}"
    end

    assert_response :success
    assert_includes response.body, "<div id=app>"
    assert_equal "text/html", response.media_type
  ensure
    FileUtils.rm_rf(dir)
  end

  test "GET /play/:code is 404 without an active session" do
    @play_session.end!
    get "/play/#{@play_session.code}"
    assert_response :not_found

    get "/play/#{@play_session.code}/info"
    assert_response :not_found
    assert_equal "not_found", json["error"]
  end

  test "joining needs a valid qr token unless the lobby is open" do
    join
    assert_response :forbidden
    assert_equal "qr_required", json["error"]

    join(qr_token: "AAAAAAAA")
    assert_response :forbidden

    join(qr_token: @play_session.qr_token_for(Time.current))
    assert_response :success
    assert_equal({ "id" => @play_session.id.to_s, "code" => @play_session.code }, json["session"])
    player = json["player"]
    assert_equal 1, player["slot"]
    assert_equal true, player["host"]
    assert_equal false, player["connected"]
    assert_equal true, player["speaker"]
    assert_equal %w[account_linked avatar color connected host id name slot speaker speaker_channel], player.keys.sort
    assert cookies[:yui_player].present?
  end

  test "an open lobby lets players join with the code only" do
    @play_session.update!(lobby_open: true)
    join
    assert_response :success
  end

  test "the player cookie is HttpOnly and SameSite=Lax" do
    @play_session.update!(lobby_open: true)
    join
    set_cookie = Array(response.headers["Set-Cookie"]).join("\n")
    assert_match(/yui_player=/, set_cookie)
    assert_match(/httponly/i, set_cookie)
    assert_match(/samesite=lax/i, set_cookie)
  end

  test "rejoining with the cookie keeps the slot and skips the qr check" do
    @play_session.update!(lobby_open: true)
    join_player(@play_session, name: "First")
    join(name: "Second")
    second = json["player"]
    @play_session.update!(lobby_open: false)

    assert_broadcasts(PlaySessionChannel.broadcasting_for(@play_session), 1) do
      join(name: "Renamed", color: "teal")
    end
    assert_response :success
    assert_equal second["id"], json["player"]["id"]
    assert_equal 2, json["player"]["slot"]
    assert_equal "Renamed", json["player"]["name"]
  end

  test "kicked players are refused" do
    @play_session.update!(lobby_open: true)
    join
    player = Player.find(json["player"]["id"])
    @play_session.kick!(player)

    join
    assert_response :forbidden
    assert_equal "kicked", json["error"]

    get "/play/#{@play_session.code}/info"
    assert_nil json["player"]
  end

  test "join and rejoin accept an avatar and broadcast it" do
    @play_session.update!(lobby_open: true)
    join(avatar: "\u{1F98A}")
    assert_response :success
    assert_equal "\u{1F98A}", json["player"]["avatar"]

    selfie = jpeg_avatar(bytes: 8.kilobytes)
    player_id = json["player"]["id"]
    assert_broadcast_on(PlaySessionChannel.broadcasting_for(@play_session),
      { type: "player_updated", player: Player.find(player_id).as_player_json.merge(avatar: selfie) }) do
      join(avatar: selfie)
    end
    assert_response :success
    assert_equal selfie, json["player"]["avatar"]

    # Leaving avatar out keeps it; null clears it.
    join(name: "Ann")
    assert_equal selfie, json["player"]["avatar"]
    join(avatar: nil)
    assert_nil json["player"]["avatar"]

    get "/play/#{@play_session.code}/info"
    assert json["player"].key?("avatar")
  end

  test "a bad or oversized avatar is a 422 validation_failed" do
    @play_session.update!(lobby_open: true)
    [ "AB", "<img src=x>", jpeg_avatar(bytes: 13.kilobytes), { "src" => "x" }, [ "\u{1F98A}" ] ].each do |avatar|
      join(avatar: avatar)
      assert_response :unprocessable_content
      assert_equal "validation_failed", json["error"]
      assert json["details"].key?("avatar")
    end

    join(avatar: "\u{1F98A}")
    assert_response :success
    join(avatar: "javascript:alert(1)")
    assert_response :unprocessable_content
    assert json["details"].key?("avatar")
  end

  test "validation errors are 422" do
    @play_session.update!(lobby_open: true)
    join(name: "", color: "black")
    assert_response :unprocessable_content
    assert_equal "validation_failed", json["error"]
    assert json["details"].key?("name")
    assert json["details"].key?("color")
  end

  test "join requires JSON and a same-origin request" do
    @play_session.update!(lobby_open: true)

    post "/play/#{@play_session.code}/join", params: { name: "Ann", color: "blue" }
    assert_response :unsupported_media_type

    join(headers: { "Origin" => "https://evil.example" })
    assert_response :forbidden
    assert_equal "forbidden_origin", json["error"]

    join(headers: { "Sec-Fetch-Site" => "cross-site" })
    assert_response :forbidden

    join(headers: { "Origin" => "http://www.example.com", "Sec-Fetch-Site" => "same-origin" })
    assert_response :success
  end

  test "join is rate limited to 10 per minute per IP" do
    10.times { join }
    assert_response :forbidden

    join
    assert_response :too_many_requests
    assert_equal "rate_limited", json["error"]
  end

  test "info reports the session, the cookie's player and taken colors" do
    @play_session.update!(lobby_open: true)
    join_player(@play_session, name: "Other", color: "red")

    get "/play/#{@play_session.code}/info"
    assert_response :success
    assert_equal @play_session.code, json["code"]
    assert_equal @play_session.kiosk.name, json["kiosk_name"]
    assert_equal true, json["lobby_open"]
    assert_nil json["player"]
    assert_equal [ "red" ], json["colors_taken"]

    join(color: "green")
    get "/play/#{@play_session.code}/info"
    assert_equal "Ann", json.dig("player", "name")
    assert_equal [ "red" ], json["colors_taken"]
  end

  test "ice requires a joined player and uses the p<id> suffix" do
    get "/play/#{@play_session.code}/ice"
    assert_response :unauthorized

    @play_session.update!(lobby_open: true)
    join
    player_id = json["player"]["id"]

    with_env("TURN_URLS" => "turn:t.example.com:3478", "TURN_SECRET" => "secret") do
      get "/play/#{@play_session.code}/ice"
    end
    assert_response :success
    turn = json["ice_servers"].last
    assert_match(/\A\d+:p#{player_id}\z/, turn["username"])
  end

  test "the pad service worker may claim the /play/ scope" do
    get "/pad/sw.js"
    assert_equal "/play/", response.headers["Service-Worker-Allowed"]

    get "/play/#{@play_session.code}/info"
    assert_nil response.headers["Service-Worker-Allowed"]
  end

  private

  def with_pad_index(path)
    previous = PlayController.pad_index
    PlayController.pad_index = path
    yield
  ensure
    PlayController.pad_index = previous
  end

  def with_env(values)
    previous = values.keys.index_with { |key| ENV[key] }
    values.each { |key, value| ENV[key] = value }
    yield
  ensure
    previous.each { |key, value| ENV[key] = value }
  end
end
