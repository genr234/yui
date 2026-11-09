require "test_helper"

class KiosksControllerTest < ActionDispatch::IntegrationTest
  setup do
    @account = Account.create!(name: "Lobby")
    @kiosk = @account.kiosks.create!(name: "Front kiosk", device_uid: "device-1", device_token_digest: Kiosk.digest_token("token"))
  end

  test "telemetry stores events and health, and the kiosk page shows them" do
    post "/api/kiosk/telemetry", as: :json, headers: { "Authorization" => "Bearer token" }, params: {
      events: [ { kind: "chrome_hung", at: 1.minute.ago.iso8601, data: { error: "debug port unreachable" } } ],
      snapshot: { chrome: "running", chrome_restarts: 2, commit: "abcdef1234", secret: "nope" }
    }
    assert_response :success
    @kiosk.reload
    assert_equal "running", @kiosk.health["chrome"]
    assert_nil @kiosk.health["secret"]
    assert_equal [ "chrome_hung" ], @kiosk.kiosk_events.pluck(:kind)

    sign_in_with_passkey
    get account_kiosk_path(@account, @kiosk)
    assert_response :success
    assert_match "Chrome running", response.body
    assert_match "debug port unreachable", response.body
  end

  test "maintenance buttons queue commands" do
    sign_in_with_passkey
    post update_now_account_kiosk_path(@account, @kiosk)
    assert_redirected_to account_kiosk_path(@account, @kiosk)
    post restart_chrome_account_kiosk_path(@account, @kiosk)
    assert_equal %w[update.apply kiosk.restartChrome], @kiosk.kiosk_commands.order(:created_at).pluck(:command_type)
  end

  test "remote input and screenshot round trip" do
    sign_in_with_passkey
    get remote_account_kiosk_path(@account, @kiosk)
    assert_response :success

    post remote_input_account_kiosk_path(@account, @kiosk), as: :json, params: { action_type: "tap", x: 2, y: 0.5 }
    command = @kiosk.kiosk_commands.find(response.parsed_body["id"])
    assert_equal({ "action" => "tap", "x" => 1.0, "y" => 0.5 }, command.payload)

    post remote_screenshot_account_kiosk_path(@account, @kiosk), as: :json, params: { max_side: 800 }
    shot = @kiosk.kiosk_commands.find(response.parsed_body["id"])
    shot.update!(status: "succeeded", result: { "image" => "data:image/jpeg;base64,AA" })
    get remote_command_account_kiosk_path(@account, @kiosk, command_id: shot.id)
    assert_equal "data:image/jpeg;base64,AA", response.parsed_body.dig("result", "image")

    post remote_input_account_kiosk_path(@account, @kiosk), as: :json, params: { action_type: "rm" }
    assert_response :unprocessable_content
  end
end
