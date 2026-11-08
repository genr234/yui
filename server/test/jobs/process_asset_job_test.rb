require "test_helper"

class ProcessAssetJobTest < ActiveSupport::TestCase
  include DiscFixtures

  setup do
    @account = Account.create!(name: "Lobby")
    @kiosks = 2.times.map do |index|
      @account.kiosks.create!(name: "Kiosk #{index}", device_uid: "device-#{index}", device_token_digest: Kiosk.digest_token("token-#{index}"))
    end
    @requested = []
  end

  test "hashes, detects the game, caches the cover and asks every kiosk to reconcile" do
    bytes = wia_image(game_id: "RMCE01", title: "MARIO KART WII")
    asset = processing_asset("game", "Mario_Kart_Wii.rvz", bytes)
    stub_gametdb("https://art.gametdb.com/wii/cover/US/RMCE01.png" => png_bytes)

    assert_difference -> { KioskCommand.where(command_type: "assets.reconcile").count }, 2 do
      ProcessAssetJob.perform_now(asset.id, "https://yui.example.com")
    end

    asset.reload
    assert_equal "ready", asset.status
    assert_nil asset.error
    assert_equal Digest::SHA256.hexdigest(bytes), asset.sha256
    assert_equal "MARIO KART WII", asset.title
    assert_equal(
      {
        "game_id" => "RMCE01", "platform" => "wii", "region" => "US", "disc_title" => "MARIO KART WII",
        "format" => "rvz", "cover_url" => "https://yui.example.com/covers/RMCE01.png"
      },
      asset.metadata
    )
    assert_equal png_bytes, File.binread(AssetStorage.covers_dir.join("RMCE01.png"))
    @kiosks.each { |kiosk| assert kiosk.kiosk_commands.exists?(command_type: "assets.reconcile", payload: {}) }
  end

  test "falls back through cover regions and keeps the uploader's title" do
    asset = processing_asset("game", "melee.iso", iso_image(game_id: "GALD01", title: "Melee", platform: :gamecube), title: "Melee (party night)")
    stub_gametdb("https://art.gametdb.com/wii/cover/EN/GALD01.png" => png_bytes)

    ProcessAssetJob.perform_now(asset.id, "https://yui.example.com")

    asset.reload
    assert_equal "Melee (party night)", asset.title
    assert_equal "EU", asset.metadata["region"]
    assert_equal "gamecube", asset.metadata["platform"]
    assert_equal %w[
      https://art.gametdb.com/wii/cover/DE/GALD01.png
      https://art.gametdb.com/wii/cover/EN/GALD01.png
    ], @requested
    assert_equal "https://yui.example.com/covers/GALD01.png", asset.metadata["cover_url"]
  end

  test "a missing cover or an unreachable GameTDB never fails the asset" do
    asset = processing_asset("game", "game.wbfs", wbfs_image(game_id: "RSPE01", title: "Wii Sports"))
    GameCover.downloader = ->(_uri) { raise Net::OpenTimeout }

    ProcessAssetJob.perform_now(asset.id, "https://yui.example.com")

    asset.reload
    assert_equal "ready", asset.status
    assert_equal "RSPE01", asset.metadata["game_id"]
    assert_nil asset.metadata["cover_url"]
  end

  test "uses the cached cover without downloading again" do
    FileUtils.mkdir_p(AssetStorage.covers_dir)
    File.binwrite(AssetStorage.covers_dir.join("RMCE01.png"), png_bytes)
    asset = processing_asset("game", "mkw.iso", iso_image(game_id: "RMCE01", title: "MKW"))
    stub_gametdb({})

    ProcessAssetJob.perform_now(asset.id, "https://yui.example.com")

    assert_empty @requested
    assert_equal "https://yui.example.com/covers/RMCE01.png", asset.reload.metadata["cover_url"]
  end

  test "the platform comes from the disc header" do
    asset = processing_asset("game", "ww.gcz", gcz_image(game_id: "GZLE01", title: "Wind Waker", platform: :gamecube))

    ProcessAssetJob.perform_now(asset.id)

    asset.reload
    assert_equal "game", asset.kind
    assert_equal "gamecube", asset.metadata["platform"]
    assert_equal "gcz", asset.metadata["format"]
  end

  test "unrecognized games are ready with an unknown format" do
    asset = processing_asset("game", "mystery.iso", "not a disc" * 100)

    ProcessAssetJob.perform_now(asset.id)

    asset.reload
    assert_equal "ready", asset.status
    assert_equal "unknown", asset.metadata["format"]
    assert_nil asset.metadata["platform"]
    assert_nil asset.metadata["game_id"]
  end

  test "non-game assets are only hashed" do
    asset = processing_asset("runtime", "dolphin.zip", "PK\x03\x04zip", metadata: { "runtime" => "dolphin", "version" => "2509", "archive" => "zip" })
    stub_gametdb({})

    ProcessAssetJob.perform_now(asset.id)

    asset.reload
    assert_equal "ready", asset.status
    assert_equal({ "runtime" => "dolphin", "version" => "2509", "archive" => "zip" }, asset.metadata)
    assert_empty @requested
  end

  test "fails the asset when the file is missing or truncated" do
    asset = processing_asset("media", "clip.mp4", "1234")
    asset.update!(byte_size: 10)

    assert_no_difference -> { KioskCommand.count } do
      ProcessAssetJob.perform_now(asset.id)
    end

    asset.reload
    assert_equal "failed", asset.status
    assert_match "expected 10", asset.error
  end

  test "skips assets that aren't processing" do
    asset = processing_asset("media", "clip.mp4", "1234")
    asset.update!(status: "uploading")

    ProcessAssetJob.perform_now(asset.id)
    assert_equal "uploading", asset.reload.status
    assert_nil ProcessAssetJob.perform_now(-1)
  end

  private

  def processing_asset(kind, filename, bytes, title: nil, metadata: {})
    asset = @account.assets.create!(
      kind: kind, filename: filename, title: title || Asset.default_title(filename),
      byte_size: bytes.bytesize, status: "processing", metadata: metadata
    )
    write_asset_file(asset, bytes)
    asset
  end

  def stub_gametdb(responses)
    GameCover.downloader = lambda do |uri|
      @requested << uri.to_s
      responses[uri.to_s]
    end
  end
end
