require "test_helper"

class KioskOperationTest < ActiveSupport::TestCase
  test "accepted operations materialize account state" do
    account = Account.create!(name: "Lobby")
    kiosk = account.kiosks.create!(
      name: "Door",
      device_uid: "door-1",
      device_token_digest: Kiosk.digest_token("token")
    )

    operation = KioskOperation.accept!(
      account: account,
      kiosk: kiosk,
      attributes: {
        client_id: "door-1",
        client_seq: 1,
        collection: "storage",
        record_id: "theme",
        action: "put",
        payload: "dark"
      }
    )

    state = account.account_state_records.find_by!(collection: "storage", record_id: "theme")
    assert_equal operation.server_seq, state.server_seq
    assert_equal "dark", state.value
  end
end
