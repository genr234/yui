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

  test "sync pull is limited and reports latest cursor" do
    account = Account.create!(name: "Lobby")
    kiosk = account.kiosks.create!(
      name: "Front kiosk",
      device_uid: "device-1",
      device_token_digest: Kiosk.digest_token("token")
    )
    251.times do |index|
      KioskOperation.accept!(
        account: account,
        kiosk: kiosk,
        attributes: {
          client_id: "seed",
          client_seq: index + 1,
          collection: "storage",
          record_id: "key-#{index}",
          action: "put",
          payload: index
        }
      )
    end

    get "/api/kiosk/sync/pull?cursor=0", headers: { "Authorization" => "Bearer token" }
    assert_response :success

    body = JSON.parse(response.body)
    assert_equal 250, body.fetch("operations").size
    assert_equal 250, body.fetch("sync_cursor")
    assert_equal true, body.fetch("has_more")

    get "/api/kiosk/sync/pull?cursor=250", headers: { "Authorization" => "Bearer token" }
    assert_response :success

    body = JSON.parse(response.body)
    assert_equal 1, body.fetch("operations").size
    assert_equal 251, body.fetch("sync_cursor")
    assert_equal false, body.fetch("has_more")
  end

  test "sync push rejects oversized batches" do
    account = Account.create!(name: "Lobby")
    account.kiosks.create!(
      name: "Front kiosk",
      device_uid: "device-1",
      device_token_digest: Kiosk.digest_token("token")
    )
    operations = 251.times.map do |index|
      {
        client_id: "device-1",
        client_seq: index + 1,
        collection: "storage",
        record_id: "key-#{index}",
        action: "put",
        payload: index
      }
    end

    post "/api/kiosk/sync/push",
      params: { operations: operations },
      headers: { "Authorization" => "Bearer token" },
      as: :json

    assert_response :content_too_large
    assert_equal 0, account.kiosk_operations.count
  end

  test "sync push rejects unknown collections" do
    account = Account.create!(name: "Lobby")
    account.kiosks.create!(
      name: "Front kiosk",
      device_uid: "device-1",
      device_token_digest: Kiosk.digest_token("token")
    )

    post "/api/kiosk/sync/push",
      params: {
        operations: [
          {
            client_id: "device-1",
            client_seq: 1,
            collection: "unexpected",
            record_id: "key",
            action: "put",
            payload: "value"
          }
        ]
      },
      headers: { "Authorization" => "Bearer token" },
      as: :json

    assert_response :unprocessable_content
    assert_equal 0, account.kiosk_operations.count
  end

  test "pairing throttles repeated invalid codes" do
    previous_cache = Rails.cache
    Rails.cache = ActiveSupport::Cache::MemoryStore.new
    account = Account.create!(name: "Lobby")
    _pairing_code, code = PairingCode.create_for!(account)

    Api::Kiosk::PairingsController::MAX_PAIRING_ATTEMPTS.times do
      post "/api/kiosk/pair", params: { code: "000000", device_uid: "device-1" }, as: :json
      assert_response :not_found
    end

    post "/api/kiosk/pair", params: { code: code, device_uid: "device-1" }, as: :json

    assert_response :too_many_requests
  ensure
    Rails.cache = previous_cache if defined?(previous_cache)
  end
end
