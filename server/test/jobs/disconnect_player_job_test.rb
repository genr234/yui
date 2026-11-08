require "test_helper"

class DisconnectPlayerJobTest < ActiveSupport::TestCase
  include PlaySessionTestHelper
  include ActionCable::TestHelper
  include ActiveJob::TestHelper

  test "closes the player's cable connections without reconnect" do
    player, = join_player(start_session)

    assert_broadcast_on("action_cable/#{player.to_gid_param}", { type: "disconnect", reconnect: false }) do
      DisconnectPlayerJob.perform_now(player)
    end
  end

  test "kicking and ending schedule the disconnect after the last message" do
    play_session = start_session
    kicked, = join_player(play_session, name: "Kicked")
    other, = join_player(play_session, name: "Other")

    freeze_time do
      assert_enqueued_with(job: DisconnectPlayerJob, args: [ kicked ], at: DisconnectPlayerJob::DELAY.from_now) do
        play_session.kick!(kicked)
      end

      assert_enqueued_jobs 1, only: DisconnectPlayerJob do
        play_session.end!
      end
      assert_enqueued_with(job: DisconnectPlayerJob, args: [ other ], at: DisconnectPlayerJob::DELAY.from_now)
    end
  end
end
