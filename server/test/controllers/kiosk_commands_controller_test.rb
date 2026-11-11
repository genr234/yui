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

  test "the web form cannot queue a console-only command" do
    sign_in_with_passkey

    account = Account.create!(name: "Lobby")
    kiosk = account.kiosks.create!(
      name: "Front kiosk",
      device_uid: "device-1",
      device_token_digest: Kiosk.digest_token("token")
    )

    assert_no_difference -> { KioskCommand.count } do
      post account_kiosk_commands_path(account), params: {
        kiosk_id: kiosk.id,
        command_type: "debug.exec",
        payload: '{"script":"whoami"}'
      }
    end
    assert_redirected_to account_path(account)
    assert kiosk.kiosk_commands.create(command_type: "debug.exec", payload: { script: "whoami" }).persisted?
  end
end
