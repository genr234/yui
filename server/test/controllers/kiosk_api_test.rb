require "test_helper"

class KioskApiTest < ActionDispatch::IntegrationTest
  test "kiosk pairs, pushes sync operations, pulls by cursor, and receives commands" do
    account = Account.create!(name: "Lobby", profile_image_url: "https://example.com/lobby.png")
    _pairing_code, code = PairingCode.create_for!(account)

    post "/api/kiosk/pair", params: {
      code: code,
      device_uid: "device-1",
      name: "Front kiosk"
    }, as: :json
    assert_response :success

    body = JSON.parse(response.body)
    token = body.fetch("device_token")
    kiosk = account.kiosks.find_by!(device_uid: "device-1")
    assert_equal "https://example.com/lobby.png", body.dig("account", "profile_image_url")

    post "/api/kiosk/sync/push",
      params: {
        operations: [
          {
            client_id: "device-1",
            client_seq: 1,
            collection: "storage",
            record_id: "welcome",
            action: "put",
            payload: "hello"
          }
        ]
      },
      headers: { "Authorization" => "Bearer #{token}" },
      as: :json
    assert_response :success
    assert_equal "hello", account.account_state_records.find_by!(collection: "storage", record_id: "welcome").value

    account.update!(profile_image_url: "https://example.com/updated.png")
    get "/api/kiosk/sync/pull?cursor=0", headers: { "Authorization" => "Bearer #{token}" }
    assert_response :success
    assert_equal "https://example.com/updated.png", JSON.parse(response.body).dig("account", "profile_image_url")

    command = kiosk.kiosk_commands.create!(command_type: "apps.uninstall", payload: { id: "old.app" })

    patch "/api/kiosk/commands/#{command.id}",
      params: { status: "succeeded", result: { ok: true } },
      headers: { "Authorization" => "Bearer #{token}" },
      as: :json
    assert_response :success
    assert_equal "succeeded", kiosk.kiosk_commands.first.reload.status
  end
end
