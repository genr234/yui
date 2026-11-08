require "test_helper"

class ApplicationCable::ConnectionTest < ActionCable::Connection::TestCase
  include PlaySessionTestHelper

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
end
