require "test_helper"

class DiscImageTest < ActiveSupport::TestCase
  include DiscFixtures

  {
    "iso" => :iso_image,
    "wbfs" => :wbfs_image,
    "rvz" => :wia_image,
    "gcz" => :gcz_image,
    "ciso" => :ciso_image
  }.each do |format, builder|
    test "reads a Wii disc header from #{format}" do
      result = read(send(builder, game_id: "RMCE01", title: "MARIO KART WII"))

      assert_equal format, result.format
      assert_equal "RMCE01", result.game_id
      assert_equal "wii", result.platform
      assert_equal "MARIO KART WII", result.disc_title
      assert_equal "US", result.region
    end

    test "reads a GameCube disc header from #{format}" do
      result = read(send(builder, game_id: "GALP01", title: "Super Smash Bros Melee", platform: :gamecube))

      assert_equal format, result.format
      assert_equal "GALP01", result.game_id
      assert_equal "gamecube", result.platform
      assert_equal "EU", result.region
    end
  end

  test "recognizes WIA as well as RVZ" do
    result = read(wia_image(magic: "WIA\x01", game_id: "RSBJ01", title: "SMASH"))
    assert_equal "wia", result.format
    assert_equal "JP", result.region
  end

  test "decodes Shift-JIS titles on Japanese discs" do
    title = "マリオ".encode(Encoding::Shift_JIS).b
    result = read(iso_image(game_id: "RMCJ01", title: title))
    assert_equal "マリオ", result.disc_title
  end

  test "unknown files and missing files are unknown, not errors" do
    assert_equal "unknown", read("just some media file" * 10).format
    assert_not read("x").detected?
    assert_equal DiscImage::UNKNOWN, DiscImage.read(AssetStorage.root.join("missing.iso"))
  end

  test "rejects a header with a game ID in the wrong format" do
    assert_not read(iso_image(game_id: "rm\x00e01", title: "Bad")).detected?
  end

  private

  def read(bytes)
    path = AssetStorage.root.join("disc-#{SecureRandom.hex(4)}.bin")
    FileUtils.mkdir_p(path.dirname)
    File.binwrite(path, bytes)
    DiscImage.read(path)
  end
end
