require "test_helper"

class ReleaseStalePlayersJobTest < ActiveSupport::TestCase
  include PlaySessionTestHelper
  include ActionCable::TestHelper

  test "frees players of every active session that went silent" do
    play_session = start_session
    player, = join_player(play_session)
    player.mark_connected!("n")

    travel Player::PRESENCE_TIMEOUT - 5.seconds do
      ReleaseStalePlayersJob.perform_now
      assert player.reload.connected
    end

    travel Player::PRESENCE_TIMEOUT + 5.seconds do
      ReleaseStalePlayersJob.perform_now
      assert_not player.reload.connected
      assert_nil player.slot
    end
  end
end
