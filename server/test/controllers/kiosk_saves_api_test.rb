require "test_helper"

class KioskSavesApiTest < ActionDispatch::IntegrationTest
  KEY = "/api/kiosk/saves/some-app/12".freeze
  ZIP = ("PK\x03\x04".b + "save-data".b * 20).freeze

  setup do
    @account = Account.create!(name: "Lobby")
    @kiosk = @account.kiosks.create!(name: "Front", device_uid: "device-1", device_token_digest: Kiosk.digest_token("token"))
    @auth = { "Authorization" => "Bearer token" }
    @other_account = Account.create!(name: "Other")
    @other_kiosk = @other_account.kiosks.create!(name: "Back", device_uid: "device-2", device_token_digest: Kiosk.digest_token("token-2"))
  end

  def put_save(body = ZIP, saved_at: "2026-11-01T10:00:00Z", headers: @auth, path: KEY, extra: {})
    put "#{path}?saved_at=#{saved_at}", params: body, headers: headers.merge("Content-Type" => "application/octet-stream").merge(extra)
  end

  test "requires a kiosk token" do
    put_save(headers: {})
    assert_response :unauthorized
    get KEY
    assert_response :unauthorized
    get "#{KEY}/download", headers: { "Authorization" => "Bearer wrong" }
    assert_response :unauthorized
  end

  test "uploads a backup as an other asset with metadata" do
    put_save
    assert_response :success

    body = JSON.parse(response.body)
    assert_equal "some-app/12", body["key"]
    assert_equal ZIP.bytesize, body["byte_size"]
    assert_equal Digest::SHA256.hexdigest(ZIP), body["sha256"]
    assert_equal "2026-11-01T10:00:00Z", body["saved_at"]

    asset = @account.assets.save_backups.sole
    assert_equal "other", asset.kind
    assert_equal "ready", asset.status
    assert_equal "some-app", asset.metadata["app_id"]
    assert_equal "12", asset.metadata["runtime"]
    assert_equal "Front", asset.metadata["kiosk_name"]
    assert_equal ZIP, File.binread(asset.file_path)

    get KEY, headers: @auth
    assert_response :success
    assert_equal asset.id.to_s, JSON.parse(response.body)["id"]

    get "#{KEY}/download", headers: @auth
    assert_response :success
    assert_equal ZIP, response.body.b
    assert_equal asset.sha256, response.headers["X-Asset-Sha256"]
  end

  test "uploading the same key replaces the backup" do
    put_save
    first = @account.assets.save_backups.sole
    newer = ZIP + "more".b
    put_save(newer, saved_at: "2026-11-02T10:00:00Z")
    assert_response :success

    assert_equal 1, @account.assets.save_backups.count
    asset = @account.assets.save_backups.sole
    assert_equal first.id, asset.id
    assert_equal newer, File.binread(asset.file_path)
    assert_equal Digest::SHA256.hexdigest(newer), asset.sha256
    assert_equal "2026-11-02T10:00:00Z", asset.metadata["saved_at"]
  end

  test "repeating an identical upload is idempotent" do
    2.times do
      put_save
      assert_response :success
    end
    assert_equal 1, @account.assets.save_backups.count
  end

  test "an older upload does not replace a newer backup" do
    put_save(saved_at: "2026-11-05T10:00:00Z")
    put_save(ZIP + "old".b, saved_at: "2026-11-01T10:00:00Z")
    assert_response :conflict
    assert_equal ZIP, File.binread(@account.assets.save_backups.sole.file_path)
  end

  test "backups are scoped to the account" do
    put_save
    get KEY, headers: { "Authorization" => "Bearer token-2" }
    assert_response :not_found
    get "#{KEY}/download", headers: { "Authorization" => "Bearer token-2" }
    assert_response :not_found

    put_save("PK-other".b, headers: { "Authorization" => "Bearer token-2" })
    assert_response :success
    assert_equal 1, @account.assets.save_backups.count
    assert_equal 1, @other_account.assets.save_backups.count
    assert_equal ZIP, File.binread(@account.assets.save_backups.sole.file_path)
  end

  test "rejects uploads above the cap" do
    stub_const(Api::Kiosk::SavesController, :MAX_BYTES, 64) do
      put_save("PK".b + "x".b * 100)
      assert_response :content_too_large
    end
    assert_equal 0, @account.assets.save_backups.count
  end

  test "rejects non-zip bodies, empty bodies and a sha mismatch" do
    put_save("not a zip")
    assert_response :unprocessable_content
    put_save("")
    assert_response :unprocessable_content
    put_save(extra: { "X-Save-Sha256" => "0" * 64 })
    assert_response :unprocessable_content
    assert_equal 0, @account.assets.save_backups.count
  end

  test "accepts a matching sha256 header" do
    put_save(extra: { "X-Save-Sha256" => Digest::SHA256.hexdigest(ZIP) })
    assert_response :success
  end

  test "rejects bad keys and timestamps" do
    put_save(path: "/api/kiosk/saves/Bad%20App/12")
    assert_response :unprocessable_content
    put_save(path: "/api/kiosk/saves/some-app/..")
    assert_response :unprocessable_content
    put_save(saved_at: "yesterday")
    assert_response :unprocessable_content
  end

  test "clamps a future saved_at to now" do
    put_save(saved_at: "2999-01-01T00:00:00Z")
    assert_response :success
    assert_operator Time.iso8601(JSON.parse(response.body)["saved_at"]), :<=, Time.current
  end

  test "backups stay out of the asset manifest and asset downloads" do
    put_save
    asset = @account.assets.save_backups.sole

    get "/api/kiosk/assets", headers: @auth
    assert_equal [], JSON.parse(response.body)["assets"]
    get "/api/kiosk/assets/#{asset.id}/download", headers: @auth
    assert_response :not_found
  end

  private

  def stub_const(klass, name, value)
    old = klass.const_get(name)
    klass.send(:remove_const, name)
    klass.const_set(name, value)
    yield
  ensure
    klass.send(:remove_const, name)
    klass.const_set(name, old)
  end
end
