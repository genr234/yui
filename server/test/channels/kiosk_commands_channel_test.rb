require "test_helper"

class KioskCommandsChannelTest < ActionCable::Channel::TestCase
  include PlaySessionTestHelper

  test "kiosks subscribe to their command stream" do
    kiosk = create_kiosk
    stub_connection current_kiosk: kiosk, current_player: nil
    subscribe

    assert subscription.confirmed?
    assert_has_stream_for kiosk
  end

  test "players are rejected" do
    player, = join_player(start_session)
    stub_connection current_kiosk: nil, current_player: player
    subscribe

    assert subscription.rejected?
  end
end
