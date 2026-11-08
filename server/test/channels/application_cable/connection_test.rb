require "test_helper"

class ApplicationCable::ConnectionTest < ActionCable::Connection::TestCase
  include PlaySessionTestHelper
  include ActionCable::TestHelper
  include ActiveJob::TestHelper

  test "identifies a kiosk by bearer token" do
    kiosk = create_kiosk(token: "kiosk-token")
    connect headers: { "Authorization" => "Bearer kiosk-token" }

    assert_equal kiosk, connection.current_kiosk
    assert_nil connection.current_player
  end

  test "identifies a player by the signed yui_player cookie" do
    player, token = join_player(start_session)
    cookies.signed[:yui_player] = Player.cookie_value(player, token)
    connect

    assert_equal player, connection.current_player
    assert_nil connection.current_kiosk
  end

  test "a simulated pad (/cable?sim=N) uses its own player cookie" do
    play_session = start_session
    player, token = join_player(play_session)
    other, other_token = join_player(play_session, name: "Bo", color: "red")
    cookies.signed[:yui_player] = Player.cookie_value(player, token)
    cookies.signed[:yui_player_sim3] = Player.cookie_value(other, other_token)

    connect "/cable?sim=3"
    assert_equal other, connection.current_player
  end

  test "rejects missing or bad credentials" do
    assert_reject_connection { connect }
    assert_reject_connection { connect headers: { "Authorization" => "Bearer wrong" } }

    player, = join_player(start_session)
    cookies.signed[:yui_player] = Player.cookie_value(player, "wrong-token")
    assert_reject_connection { connect }
  end

  test "rejects unsigned cookies" do
    player, token = join_player(start_session)
    cookies[:yui_player] = Player.cookie_value(player, token)
    assert_reject_connection { connect }
  end

  test "production origin check allows only an authenticated kiosk with no Origin" do
    create_kiosk(token: "kiosk-token")
    player, token = join_player(start_session)
    cookies.signed[:yui_player] = Player.cookie_value(player, token)

    with_production_cable_origins do
      assert cable_origin_allowed?("HTTP_AUTHORIZATION" => "Bearer kiosk-token"),
        "an authenticated kiosk with no Origin is a non-browser handshake"
      assert cable_origin_allowed?(
        "HTTP_AUTHORIZATION" => "Bearer kiosk-token",
        "HTTP_ORIGIN" => "https://www.example.com"
      )
      assert_not cable_origin_allowed?(
        "HTTP_AUTHORIZATION" => "Bearer kiosk-token",
        "HTTP_ORIGIN" => "https://evil.example"
      ), "the Go client sends the server Origin; a foreign Origin is rejected"

      assert_not cable_origin_allowed?("HTTP_AUTHORIZATION" => "Bearer wrong")
      assert_not cable_origin_allowed?("HTTP_AUTHORIZATION" => "Bearer ")
      assert_not cable_origin_allowed?(
        "HTTP_AUTHORIZATION" => "Bearer wrong",
        "HTTP_ORIGIN" => "https://evil.example",
        "HTTP_COOKIE" => "yui_player=present"
      ), "an invalid bearer plus a player cookie must not skip a foreign Origin"
      assert_not cable_origin_allowed?(
        { "HTTP_AUTHORIZATION" => "Bearer wrong", "HTTP_ORIGIN" => "https://evil.example" },
        cookies
      )

      assert cable_origin_allowed?("HTTP_ORIGIN" => "https://www.example.com")
      assert cable_origin_allowed?({ "HTTP_ORIGIN" => "https://www.example.com" }, cookies)
      assert_not cable_origin_allowed?({})
      assert_not cable_origin_allowed?("HTTP_ORIGIN" => "https://evil.example")
      assert_not cable_origin_allowed?({ "HTTP_ORIGIN" => "https://evil.example" }, cookies)
    end
  end

  test "rejects kicked players and ended sessions" do
    play_session = start_session
    kicked, kicked_token = join_player(play_session, name: "Kicked")
    play_session.kick!(kicked)
    cookies.signed[:yui_player] = Player.cookie_value(kicked, kicked_token)
    assert_reject_connection { connect }

    other_session = start_session
    player, token = join_player(other_session)
    other_session.end!
    cookies.signed[:yui_player] = Player.cookie_value(player, token)
    assert_reject_connection { connect }
  end

  test "a kick closes the player's open connection, which can't come back" do
    play_session = start_session
    player, token = join_player(play_session)
    cookies.signed[:yui_player] = Player.cookie_value(player, token)
    connect

    internal_channel = "action_cable/#{connection.connection_identifier}"
    assert_broadcast_on(internal_channel, { type: "disconnect", reconnect: false }) do
      perform_enqueued_jobs { play_session.kick!(player) }
    end
    assert_reject_connection { connect }
  end

  private
    def with_production_cable_origins
      config = ActionCable.server.config
      previous = [
        config.disable_request_forgery_protection,
        config.allow_same_origin_as_host,
        config.allowed_request_origins
      ]
      config.disable_request_forgery_protection = false
      config.allow_same_origin_as_host = true
      config.allowed_request_origins = []
      yield
    ensure
      config.disable_request_forgery_protection, config.allow_same_origin_as_host, config.allowed_request_origins = previous
    end

    def cable_origin_allowed?(headers, cookie_jar = nil)
      env = Rack::MockRequest.env_for(
        "https://www.example.com/cable",
        {
          "HTTPS" => "on",
          "HTTP_HOST" => "www.example.com",
          "HTTP_CONNECTION" => "upgrade",
          "HTTP_UPGRADE" => "websocket",
          "HTTP_SEC_WEBSOCKET_KEY" => Base64.strict_encode64("0123456789abcdef"),
          "HTTP_SEC_WEBSOCKET_VERSION" => "13"
        }.merge(headers)
      )
      connection = ApplicationCable::Connection.new(ActionCable.server, env)
      connection.send(:request).cookie_jar = cookie_jar if cookie_jar
      connection.allow_request_origin?
    end
end
