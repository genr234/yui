require "test_helper"

class AssetTest < ActiveSupport::TestCase
  include DiscFixtures

  setup do
    @account = Account.create!(name: "Lobby")
  end

  test "sanitizes filenames to a single safe path segment" do
    assert_equal "passwd", Asset.sanitize_filename("../../etc/passwd")
    assert_equal "evil.iso", Asset.sanitize_filename("C:\\games\\evil.iso")
    assert_equal "Mario Kart (USA) [RMCE01].rvz", Asset.sanitize_filename("Mario Kart (USA) [RMCE01].rvz")
    assert_equal "a_b_c.zip", Asset.sanitize_filename("a;b|c.zip")
    assert_equal "hidden", Asset.sanitize_filename(".hidden")
    assert_equal "upload.bin", Asset.sanitize_filename("..")
    assert_equal "upload.bin", Asset.sanitize_filename("")
    assert_equal "_.iso", Asset.sanitize_filename("\xFF.iso".b)
    assert_operator Asset.sanitize_filename("x" * 500).bytesize, :<=, Asset::MAX_FILENAME_BYTES
  end

  test "validates kind, status and sanitizes the stored filename" do
    asset = @account.assets.new(kind: "console", status: "done", title: "x", filename: "../a.iso")
    assert_not asset.valid?
    assert asset.errors.key?(:kind)
    assert asset.errors.key?(:status)
    assert_equal "a.iso", asset.filename

    asset = @account.assets.create!(kind: "media", title: "Trailer", filename: "trailer.mp4", byte_size: 3)
    assert_equal "uploading", asset.status
    assert_equal({}, asset.metadata)
    assert_equal AssetStorage.assets_dir.join(@account.id.to_s, asset.id.to_s, "trailer.mp4"), asset.file_path
  end

  test "destroying an asset removes its files" do
    asset = @account.assets.create!(kind: "media", title: "Clip", filename: "clip.mp4", byte_size: 4)
    write_asset_file(asset, "data")
    assert asset.file_path.file?

    asset.destroy!
    assert_not asset.directory.exist?
  end

  test "destroying a ready asset asks every kiosk on the account to reconcile" do
    kiosks = 2.times.map { |index| create_kiosk(@account, index) }
    create_kiosk(Account.create!(name: "Other"), 9)
    asset = @account.assets.create!(kind: "other", title: "Doc", filename: "doc.pdf", status: "ready", byte_size: 1)

    assert_difference -> { KioskCommand.where(command_type: "assets.reconcile").count }, 2 do
      asset.destroy!
    end
    kiosks.each do |kiosk|
      assert_equal({}, kiosk.kiosk_commands.find_by!(command_type: "assets.reconcile").payload)
    end
  end

  test "destroying an unfinished upload doesn't notify kiosks" do
    create_kiosk(@account, 1)
    asset = @account.assets.create!(kind: "other", title: "Doc", filename: "doc.pdf", byte_size: 10)

    assert_no_difference -> { KioskCommand.count } do
      asset.destroy!
    end
  end

  test "kiosk assets are unique per kiosk and go away with the kiosk or the asset" do
    kiosk = create_kiosk(@account, 1)
    asset = @account.assets.create!(kind: "other", title: "Doc", filename: "doc.pdf", status: "ready", byte_size: 1)
    kiosk.kiosk_assets.create!(asset: asset, state: "downloading", bytes_done: 1)

    duplicate = kiosk.kiosk_assets.new(asset: asset, state: "queued")
    assert_not duplicate.valid?
    assert_not kiosk.kiosk_assets.new(asset: asset, state: "sleeping").valid?

    kiosk.destroy!
    assert_equal 0, KioskAsset.count
  end

  test "kiosk json matches the contract" do
    asset = @account.assets.create!(
      kind: "game", title: "Mario Kart Wii", filename: "mkw.rvz", status: "ready", byte_size: 42,
      sha256: "a" * 64,
      metadata: { "game_id" => "RMCE01", "platform" => "wii", "region" => "US", "disc_title" => "MARIO KART WII", "format" => "rvz", "cover_url" => nil }
    )
    json = asset.as_kiosk_json(base_url: "https://yui.example.com")
    assert_equal asset.id.to_s, json[:id]
    assert_equal %i[id kind title filename byte_size sha256 metadata updated_at], json.keys
    assert_nil json[:metadata]["cover_url"]

    FileUtils.mkdir_p(AssetStorage.covers_dir)
    File.binwrite(AssetStorage.covers_dir.join("RMCE01.png"), png_bytes)
    assert_equal "https://yui.example.com/covers/RMCE01.png", asset.as_kiosk_json(base_url: "https://yui.example.com")[:metadata]["cover_url"]
  end

  test "default title strips the extension" do
    assert_equal "Mario Kart Wii", Asset.default_title("Mario_Kart_Wii.rvz")
  end

  test "stale upload cleanup removes week-old unfinished uploads only" do
    stale = @account.assets.create!(kind: "other", title: "Old", filename: "old.bin", byte_size: 10)
    write_asset_file(stale, "12345")
    stale.update_columns(updated_at: 8.days.ago)
    fresh = @account.assets.create!(kind: "other", title: "New", filename: "new.bin", byte_size: 10)
    done = @account.assets.create!(kind: "other", title: "Done", filename: "done.bin", byte_size: 1, status: "ready")
    done.update_columns(updated_at: 30.days.ago)

    CleanupStaleUploadsJob.perform_now

    assert_not Asset.exists?(stale.id)
    assert_not stale.directory.exist?
    assert Asset.exists?(fresh.id)
    assert Asset.exists?(done.id)
  end

  private

  def create_kiosk(account, index)
    account.kiosks.create!(name: "Kiosk #{index}", device_uid: "device-#{index}", device_token_digest: Kiosk.digest_token("token-#{account.id}-#{index}"))
  end
end
