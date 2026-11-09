require "test_helper"

# An admin attaches a media image to a game as its cover (metadata.cover_for).
class AssetCoverForTest < ActionDispatch::IntegrationTest
  include MediaFixtures

  setup do
    sign_in_with_passkey
    @account = Account.create!(name: "Lobby")
    @kiosk = @account.kiosks.create!(name: "Front", device_uid: "d1", device_token_digest: Kiosk.digest_token("token"))
    @game = ready_asset("game", "tiny.nes", "Tiny Quest", nes_rom)
    @image = ready_asset("media", "box.png", "Box art", PNG)
  end

  def ready_asset(kind, filename, title, bytes)
    asset = @account.assets.create!(kind: kind, title: title, filename: filename, byte_size: bytes.bytesize, status: "ready", sha256: Digest::SHA256.hexdigest(bytes))
    FileUtils.mkdir_p(asset.directory)
    File.binwrite(asset.file_path, bytes)
    asset
  end

  def manifest_game
    get "/api/kiosk/assets", headers: { "Authorization" => "Bearer token" }
    JSON.parse(response.body)["assets"].find { |asset| asset["id"] == @game.id.to_s }
  end

  test "attaching an image gives the game a cover_url in the kiosk manifest" do
    assert_nil manifest_game.dig("metadata", "cover_url")

    assert_difference -> { @kiosk.kiosk_commands.where(command_type: "assets.reconcile").count }, 1 do
      patch account_asset_path(@account, @image), params: { cover_for: @game.id }
    end
    assert_redirected_to account_path(@account, anchor: "asset-library")
    assert_equal @game.id.to_s, @image.reload.metadata["cover_for"]

    url = manifest_game.dig("metadata", "cover_url")
    assert_match(%r{\Ahttp://www\.example\.com/media_covers/\h{32}\.png\z}, url)
    get URI(url).path
    assert_response :success
    assert_equal PNG, response.body.b
  end

  test "an existing cover wins" do
    @game.update!(metadata: { "cover_url" => "https://example.com/own.png" })
    patch account_asset_path(@account, @image), params: { cover_for: @game.id }
    assert_equal "https://example.com/own.png", manifest_game.dig("metadata", "cover_url")
  end

  test "blank cover_for detaches" do
    patch account_asset_path(@account, @image), params: { cover_for: @game.id }
    patch account_asset_path(@account, @image), params: { cover_for: "" }
    assert_nil @image.reload.metadata["cover_for"]
    assert_nil manifest_game.dig("metadata", "cover_url")
  end

  test "only images, only games of this account" do
    other_game = Account.create!(name: "Other").assets.create!(kind: "game", title: "X", filename: "x.nes", byte_size: 1)
    patch account_asset_path(@account, @image), params: { cover_for: other_game.id }
    assert_nil @image.reload.metadata["cover_for"]

    song = ready_asset("media", "song.mp3", "Song", "id3".b)
    patch account_asset_path(@account, song), params: { cover_for: @game.id }
    assert_nil song.reload.metadata["cover_for"]

    patch account_asset_path(@account, @image), params: { cover_for: @image.id }
    assert_nil @image.reload.metadata["cover_for"]
  end

  test "the library offers the picker on image rows" do
    get account_assets_path(@account)
    assert_select "form[action=?] select[name='cover_for'] option[value=?]", account_asset_path(@account, @image), @game.id.to_s
  end

  test "processing a rom and a song fills in metadata" do
    rom = @account.assets.create!(kind: "game", title: "Tiny", filename: "Tiny.gb", byte_size: game_boy_rom.bytesize, status: "processing")
    FileUtils.mkdir_p(rom.directory)
    File.binwrite(rom.file_path, game_boy_rom)
    song_bytes = flac_file
    song = @account.assets.create!(kind: "media", title: "song", filename: "song.flac", byte_size: song_bytes.bytesize, status: "processing")
    FileUtils.mkdir_p(song.directory)
    File.binwrite(song.file_path, song_bytes)

    ProcessAssetJob.perform_now(rom.id, "https://yui.example.com")
    ProcessAssetJob.perform_now(song.id, "https://yui.example.com")

    assert_equal "gb", rom.reload.metadata["platform"]
    assert_equal "TINY LAND", rom.title
    assert_equal "Flac Tune", song.reload.title
    assert_equal "audio", song.metadata["kind"]
    assert_match(%r{\Ahttps://yui\.example\.com/media_covers/}, song.metadata["cover_url"])
  end
end
