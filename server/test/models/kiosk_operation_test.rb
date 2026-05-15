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

  test "duplicate client sequence returns existing account operation" do
    account = Account.create!(name: "Lobby")
    kiosk = account.kiosks.create!(
      name: "Door",
      device_uid: "door-1",
      device_token_digest: Kiosk.digest_token("token")
    )

    first = KioskOperation.accept!(
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
    second = KioskOperation.accept!(
      account: account,
      kiosk: kiosk,
      attributes: {
        client_id: "door-1",
        client_seq: 1,
        collection: "storage",
        record_id: "theme",
        action: "put",
        payload: "light"
      }
    )

    assert_equal first, second
    assert_equal 1, account.kiosk_operations.count
    assert_equal "dark", account.account_state_records.find_by!(collection: "storage", record_id: "theme").value
  end

  test "client sequence uniqueness is scoped to account" do
    first_account = Account.create!(name: "First")
    second_account = Account.create!(name: "Second")
    first_kiosk = first_account.kiosks.create!(
      name: "First kiosk",
      device_uid: "device-1",
      device_token_digest: Kiosk.digest_token("first-token")
    )
    second_kiosk = second_account.kiosks.create!(
      name: "Second kiosk",
      device_uid: "device-2",
      device_token_digest: Kiosk.digest_token("second-token")
    )

    first = KioskOperation.accept!(
      account: first_account,
      kiosk: first_kiosk,
      attributes: {
        client_id: "shared-client",
        client_seq: 1,
        collection: "storage",
        record_id: "theme",
        action: "put",
        payload: "dark"
      }
    )
    second = KioskOperation.accept!(
      account: second_account,
      kiosk: second_kiosk,
      attributes: {
        client_id: "shared-client",
        client_seq: 1,
        collection: "storage",
        record_id: "theme",
        action: "put",
        payload: "light"
      }
    )

    assert_not_equal first.account_id, second.account_id
    assert_equal 1, first.server_seq
    assert_equal 1, second.server_seq
  end
end
