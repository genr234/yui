require "test_helper"

class EndAbandonedPlaySessionsJobTest < ActiveSupport::TestCase
  include PlaySessionTestHelper
  include ActionCable::TestHelper
  include ActiveJob::TestHelper

  test "ends sessions once their kiosk has been silent for KIOSK_TIMEOUT" do
    kiosk = create_kiosk
    kiosk.touch_seen!
    play_session = start_session(kiosk)
    player, = join_player(play_session)

    travel PlaySession::KIOSK_TIMEOUT - 1.minute do
      EndAbandonedPlaySessionsJob.perform_now
      assert play_session.reload.active?
    end

    travel PlaySession::KIOSK_TIMEOUT + 1.minute do
      assert_broadcast_on(PlaySessionChannel.broadcasting_for(player), { type: "session_ended" }) do
        EndAbandonedPlaySessionsJob.perform_now
      end
      assert_not play_session.reload.active?
      assert_enqueued_with(job: DisconnectPlayerJob, args: [ player ])
    end
  end

  test "keeps sessions whose kiosk is still seen" do
    kiosk = create_kiosk
    kiosk.touch_seen!
    play_session = start_session(kiosk)

    travel PlaySession::KIOSK_TIMEOUT + 1.minute do
      kiosk.touch_seen!
      EndAbandonedPlaySessionsJob.perform_now
    end

    assert play_session.reload.active?
  end

  test "ends an old session whose kiosk was never seen, but not a fresh one" do
    old_session = start_session
    fresh_session = nil

    travel PlaySession::KIOSK_TIMEOUT + 1.minute do
      fresh_session = start_session
      EndAbandonedPlaySessionsJob.perform_now
    end

    assert_not old_session.reload.active?
    assert fresh_session.reload.active?
  end
end
