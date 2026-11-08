require "test_helper"

class AccountPlaySessionsTest < ActionDispatch::IntegrationTest
  include PlaySessionTestHelper

  test "the account page lists active play sessions and their players" do
    sign_in_with_passkey
    kiosk = create_kiosk(name: "Arcade kiosk")
    play_session = start_session(kiosk)
    join_player(play_session, name: "Ann")
    kicked, = join_player(play_session, name: "Gone")
    play_session.kick!(kicked)

    get account_path(kiosk.account)
    assert_response :success
    assert_includes response.body, play_session.code
    assert_includes response.body, "P1 Ann (host)"
    assert_not_includes response.body, "Gone"

    play_session.end!
    get account_path(kiosk.account)
    assert_not_includes response.body, "Play Sessions"
  end
end
