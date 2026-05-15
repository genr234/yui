require "test_helper"

class KioskCommandsControllerTest < ActionDispatch::IntegrationTest
  test "queues uninstall command from app id shorthand" do
    sign_in_with_passkey

    account = Account.create!(name: "Lobby")
    kiosk = account.kiosks.create!(
      name: "Front kiosk",
      device_uid: "device-1",
      device_token_digest: Kiosk.digest_token("token")
    )

    post account_kiosk_commands_path(account), params: {
      kiosk_id: kiosk.id,
      command_type: "apps.uninstall",
      payload: "{dev.genr.links}"
    }

    assert_redirected_to account_path(account)
    command = kiosk.kiosk_commands.last
    assert_equal "apps.uninstall", command.command_type
    assert_equal({ "id" => "dev.genr.links" }, command.payload)
  end
end
