require "test_helper"

class KioskAssetsApiTest < ActionDispatch::IntegrationTest
  include DiscFixtures

  DATA = (0..255).map(&:chr).join.b * 4 # 1024 bytes

  setup do
    @account = Account.create!(name: "Lobby")
    @kiosk = @account.kiosks.create!(name: "Front", device_uid: "device-1", device_token_digest: Kiosk.digest_token("token"))
    @auth = { "Authorization" => "Bearer token" }
    @asset = @account.assets.create!(
      kind: "game", title: "Mario Kart Wii", filename: "mkw.rvz", byte_size: DATA.bytesize, status: "ready",
      sha256: Digest::SHA256.hexdigest(DATA),
      metadata: { "game_id" => "RMCE01", "platform" => "wii", "region" => "US", "disc_title" => "MARIO KART WII", "format" => "rvz", "cover_url" => nil }
    )
    write_asset_file(@asset, DATA)

    other_account = Account.create!(name: "Other")
    @foreign = other_account.assets.create!(kind: "media", title: "Secret", filename: "secret.mp4", byte_size: 4, status: "ready", sha256: "b" * 64)
    write_asset_file(@foreign, "nope")
  end

  test "requires a kiosk token" do
    get "/api/kiosk/assets"
    assert_response :unauthorized
    get "/api/kiosk/assets/#{@asset.id}/download", headers: { "Authorization" => "Bearer wrong" }
    assert_response :unauthorized
    patch "/api/kiosk/assets/#{@asset.id}/status", params: { state: "ready" }, as: :json
    assert_response :unauthorized
  end

  test "lists only ready assets of the kiosk's account" do
    @account.assets.create!(kind: "media", title: "Uploading", filename: "up.mp4", byte_size: 10)
    @account.assets.create!(kind: "media", title: "Broken", filename: "broken.mp4", byte_size: 10, status: "failed")
    FileUtils.mkdir_p(AssetStorage.covers_dir)
    File.binwrite(AssetStorage.covers_dir.join("RMCE01.png"), png_bytes)

    get "/api/kiosk/assets", headers: @auth
    assert_response :success

    assets = JSON.parse(response.body).fetch("assets")
    assert_equal [ @asset.id.to_s ], assets.map { |asset| asset["id"] }
    asset = assets.first
    assert_equal "game", asset["kind"]
    assert_equal "mkw.rvz", asset["filename"]
    assert_equal DATA.bytesize, asset["byte_size"]
    assert_equal @asset.sha256, asset["sha256"]
    assert_equal "RMCE01", asset.dig("metadata", "game_id")
    assert_equal "http://www.example.com/covers/RMCE01.png", asset.dig("metadata", "cover_url")
    assert asset["updated_at"].present?
  end

  test "downloads the whole file" do
    get "/api/kiosk/assets/#{@asset.id}/download", headers: @auth
    assert_response :ok
    assert_equal DATA, response.body.b
    assert_equal "bytes", response.headers["Accept-Ranges"]
    assert_equal DATA.bytesize.to_s, response.headers["Content-Length"]
    assert_equal @asset.sha256, response.headers["X-Asset-Sha256"]
  end

  test "honours open-ended and bounded ranges" do
    get "/api/kiosk/assets/#{@asset.id}/download", headers: @auth.merge("Range" => "bytes=1000-")
    assert_response :partial_content
    assert_equal DATA.byteslice(1000..), response.body.b
    assert_equal "bytes 1000-1023/1024", response.headers["Content-Range"]
    assert_equal "24", response.headers["Content-Length"]
    assert_equal @asset.sha256, response.headers["X-Asset-Sha256"]

    get "/api/kiosk/assets/#{@asset.id}/download", headers: @auth.merge("Range" => "bytes=10-19")
    assert_response :partial_content
    assert_equal DATA.byteslice(10, 10), response.body.b
    assert_equal "bytes 10-19/1024", response.headers["Content-Range"]
    assert_equal "10", response.headers["Content-Length"]

    get "/api/kiosk/assets/#{@asset.id}/download", headers: @auth.merge("Range" => "bytes=1020-5000")
    assert_response :partial_content
    assert_equal "bytes 1020-1023/1024", response.headers["Content-Range"]

    get "/api/kiosk/assets/#{@asset.id}/download", headers: @auth.merge("Range" => "bytes=-4")
    assert_response :partial_content
    assert_equal DATA.byteslice(-4, 4), response.body.b
  end

  test "rejects unsatisfiable ranges and ignores malformed ones" do
    get "/api/kiosk/assets/#{@asset.id}/download", headers: @auth.merge("Range" => "bytes=1024-")
    assert_response :range_not_satisfiable
    assert_equal "bytes */1024", response.headers["Content-Range"]

    get "/api/kiosk/assets/#{@asset.id}/download", headers: @auth.merge("Range" => "bytes=0-1,5-6")
    assert_response :ok
    assert_equal DATA.bytesize, response.body.bytesize
  end

  test "404s for other accounts' assets and unfinished assets" do
    get "/api/kiosk/assets/#{@foreign.id}/download", headers: @auth
    assert_response :not_found

    uploading = @account.assets.create!(kind: "media", title: "Up", filename: "up.mp4", byte_size: 10)
    get "/api/kiosk/assets/#{uploading.id}/download", headers: @auth
    assert_response :not_found

    patch "/api/kiosk/assets/#{@foreign.id}/status", params: { state: "ready", bytes_done: 4 }, headers: @auth, as: :json
    assert_response :not_found
    assert_equal 0, KioskAsset.count
  end

  test "status reports upsert one KioskAsset per kiosk and asset" do
    patch "/api/kiosk/assets/#{@asset.id}/status", params: { state: "downloading", bytes_done: 512, error: nil }, headers: @auth, as: :json
    assert_response :no_content
    record = @kiosk.kiosk_assets.sole
    assert_equal [ "downloading", 512, nil ], [ record.state, record.bytes_done, record.error ]

    patch "/api/kiosk/assets/#{@asset.id}/status", params: { state: "failed", bytes_done: 1024, error: "checksum mismatch" }, headers: @auth, as: :json
    assert_response :no_content
    record = @kiosk.kiosk_assets.sole
    assert_equal [ "failed", 1024, "checksum mismatch" ], [ record.state, record.bytes_done, record.error ]

    patch "/api/kiosk/assets/#{@asset.id}/status", params: { state: "exploded" }, headers: @auth, as: :json
    assert_response :unprocessable_content
  end

  test "serves cached covers publicly with strict IDs" do
    FileUtils.mkdir_p(AssetStorage.covers_dir)
    File.binwrite(AssetStorage.covers_dir.join("RMCE01.png"), png_bytes)

    get "/covers/RMCE01.png"
    assert_response :ok
    assert_equal "image/png", response.media_type
    assert_equal png_bytes, response.body.b

    get "/covers/RSPE01.png"
    assert_response :not_found

    get "/covers/rmce01.png"
    assert_response :not_found

    get "/covers/..%2FRMCE01.png"
    assert_response :not_found
  end

  test "on-demand games are listed with a remote flag and can be requested" do
    linked = LinkedAccount.new(account: @account, provider: "romm", label: "Home RomM", base_url: "https://romm.example.test")
    linked.assign_credentials(auth: "password", username: "kim", password: "secret")
    linked.save!
    remote = @account.assets.create!(kind: "game", title: "Tiny Quest", filename: "tiny.sfc", byte_size: 64, status: "remote",
      linked_account: linked, metadata: { "platform" => "snes", "source" => { "remote_id" => "10" } })

    get "/api/kiosk/assets", headers: @auth
    item = response.parsed_body["assets"].find { |entry| entry["id"] == remote.id.to_s }
    assert item["remote"]
    assert_nil item["sha256"]

    get "/api/kiosk/assets/#{remote.id}/download", headers: @auth
    assert_response :not_found

    assert_enqueued_jobs 1, only: LinkedFetchJob do
      post "/api/kiosk/assets/#{remote.id}/request", headers: @auth
    end
    assert_response :accepted
    assert_equal "fetching", response.parsed_body["state"]

    post "/api/kiosk/assets/#{@asset.id}/request", headers: @auth
    assert_response :success
    assert_equal "ready", response.parsed_body["state"]

    post "/api/kiosk/assets/#{@foreign.id}/request", headers: @auth
    assert_response :not_found
  end
end
