require "test_helper"

class AccountActivityTest < ActionDispatch::IntegrationTest
  test "clears recent commands from the account view without deleting them" do
    sign_in_with_passkey
    account = Account.create!(name: "Lobby")
    kiosk = account.kiosks.create!(
      name: "Front kiosk",
      device_uid: "device-1",
      device_token_digest: Kiosk.digest_token("token")
    )
    command = kiosk.kiosk_commands.create!(command_type: "apps.uninstall", payload: { id: "old.app" })

    get account_path(account)
    assert_response :success
    assert_includes response.body, "apps.uninstall"
    assert_select ".plain-list--ordered li", 1

    assert_no_difference -> { KioskCommand.count } do
      delete recent_commands_account_path(account)
    end

    assert_redirected_to account_path(account)
    assert command.reload.persisted?

    get account_path(account)
    assert_response :success
    assert_select ".plain-list--ordered li", 0
    assert_includes response.body, "No commands have been queued yet."
  end

  test "clears recent logs from the account view without deleting sync operations" do
    sign_in_with_passkey
    account = Account.create!(name: "Lobby")
    kiosk = account.kiosks.create!(
      name: "Front kiosk",
      device_uid: "device-1",
      device_token_digest: Kiosk.digest_token("token")
    )
    operation = KioskOperation.accept!(
      account: account,
      kiosk: kiosk,
      attributes: {
        client_id: "device-1",
        client_seq: 1,
        collection: "storage",
        record_id: "welcome",
        action: "put",
        payload: "hello"
      }
    )

    get account_path(account)
    assert_response :success
    assert_includes response.body, "storage/welcome"

    assert_no_difference -> { KioskOperation.count } do
      delete recent_operations_account_path(account)
    end

    assert_redirected_to account_path(account)
    assert operation.reload.persisted?

    get account_path(account)
    assert_response :success
    assert_not_includes response.body, "storage/welcome"
    assert_includes response.body, "Device logs will appear here as they sync."
  end
end
