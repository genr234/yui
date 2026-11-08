require "test_helper"

class AssetsControllerTest < ActionDispatch::IntegrationTest
  include DiscFixtures

  setup do
    sign_in_with_passkey
    @account = Account.create!(name: "Lobby")
    @kiosks = [ "Kiosk 1", "Kiosk 2", "Kiosk 3" ].each_with_index.map do |name, index|
      @account.kiosks.create!(name: name, device_uid: "device-#{index}", device_token_digest: Kiosk.digest_token("token-#{index}"))
    end
  end

  test "account page shows the upload form and the asset library with per-kiosk sync" do
    game = @account.assets.create!(
      kind: "wii-game", title: "Mario Kart Wii", filename: "mkw.rvz", byte_size: 3_650_722_201, status: "ready", sha256: "a" * 64,
      metadata: { "game_id" => "RMCE01", "format" => "rvz" }
    )
    FileUtils.mkdir_p(AssetStorage.covers_dir)
    File.binwrite(AssetStorage.covers_dir.join("RMCE01.png"), png_bytes)
    @kiosks[0].kiosk_assets.create!(asset: game, state: "downloading", bytes_done: 2_254_857_830)
    @kiosks[1].kiosk_assets.create!(asset: game, state: "failed", error: "disk full")
    upload = @account.assets.create!(kind: "media", title: "Trailer", filename: "trailer.mp4", byte_size: 100)
    write_asset_file(upload, "x" * 40)

    get account_path(@account)
    assert_response :success

    assert_select "form[data-controller='asset-upload'][data-asset-upload-endpoint-value=?]", account_asset_uploads_url(@account)
    assert_select "select[name='kind'] option[value='runtime']"
    assert_select "[data-asset-upload-target='runtimeFields'][hidden]"
    assert_select "turbo-frame#asset_library" do
      assert_select "[data-asset-library-active='true']"
      assert_select "##{ActionView::RecordIdentifier.dom_id(game)}" do
        assert_select "strong", "Mario Kart Wii"
        assert_select "img[src='/covers/RMCE01.png']"
        assert_select ".asset-sync__kiosk", "Kiosk 1: 2.1 / 3.4 GB"
        assert_select ".asset-sync__kiosk", "Kiosk 2: failed: disk full"
        assert_select ".asset-sync__kiosk", "Kiosk 3: waiting"
        assert_select ".quiet-status", "ready"
      end
      assert_select "##{ActionView::RecordIdentifier.dom_id(upload)}" do
        assert_select "progress[value='40'][max='100']"
        assert_select ".quiet-status", "uploading"
      end
    end
  end

  test "the list frame stops polling when nothing is moving" do
    game = @account.assets.create!(kind: "other", title: "Doc", filename: "doc.pdf", byte_size: 10, status: "ready", sha256: "a" * 64)
    @kiosks.each { |kiosk| kiosk.kiosk_assets.create!(asset: game, state: "ready", bytes_done: 10) }

    get account_assets_path(@account)
    assert_response :success
    assert_select "turbo-frame#asset_library [data-asset-library-active='false']"
    assert_select ".asset-sync__kiosk", "Kiosk 1: ready"
  end

  test "deleting an asset removes its files and tells kiosks to reconcile" do
    game = @account.assets.create!(kind: "other", title: "Doc", filename: "doc.pdf", byte_size: 4, status: "ready", sha256: "a" * 64)
    write_asset_file(game, "data")

    assert_difference -> { KioskCommand.where(command_type: "assets.reconcile").count }, 3 do
      delete account_asset_path(@account, game)
    end
    assert_redirected_to account_path(@account, anchor: "asset-library")
    assert_not Asset.exists?(game.id)
    assert_not game.directory.exist?
  end

  test "can't delete another account's asset" do
    other = Account.create!(name: "Other")
    asset = other.assets.create!(kind: "other", title: "Doc", filename: "doc.pdf", byte_size: 4, status: "ready")

    delete account_asset_path(@account, asset)
    assert_response :not_found
    assert Asset.exists?(asset.id)
  end
end
