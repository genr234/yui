require "test_helper"
require_relative "../support/wiiu_fixtures"

class WiiUTitleTest < ActiveSupport::TestCase
  include WiiuFixtures
  include DiscFixtures
  include MediaFixtures

  setup { @dir = Pathname(Dir.mktmpdir("wiiu")) }
  teardown { FileUtils.rm_rf(@dir) }

  def analyze(name, bytes)
    path = @dir.join(name)
    File.binwrite(path, bytes)
    AssetAnalyzers::WiiUTitle.call(path.to_s, nil)
  end

  test "wua: reads the base game title id from the ZArchive root" do
    result = analyze("Nintendo_Land.wua", wua_image)
    assert_equal "wiiu", result.metadata["platform"]
    assert_equal "wua", result.metadata["format"]
    assert_equal "0005000010101234", result.metadata["title_id"]
    assert_equal true, result.metadata["playable"]
    assert_equal "Nintendo Land", result.metadata["title"]
    assert_nil result.title
    assert_match(/\Ahsl\(\d+ 55% 45%\)\z/, result.metadata["cover_color"])
  end

  test "wua: update and DLC directories are skipped, the base game wins in any order" do
    result = analyze("a.wua", wua_image(children: [ [ "0005000e10101234_v32", :dir ], [ "0005000c10101234_v1", :dir ], [ "0005000010101234_v16", :dir ] ]))
    assert_equal "0005000010101234", result.metadata["title_id"]
    only_update = analyze("a.wua", wua_image(children: [ [ "0005000e10101234_v32", :dir ] ]))
    assert_equal "wua", only_update.metadata["format"]
    assert_not only_update.metadata.key?("title_id")
  end

  test "wua: files and oddly named entries are ignored; long names use the two byte length" do
    long = "x" * 200
    result = analyze("a.wua", wua_image(children: [ [ "0005000010101234_v16", :file ], [ long, :dir ], [ "readme", :dir ], [ "0005000010abcdef_v0", :dir ] ]))
    assert_equal "0005000010abcdef", result.metadata["title_id"]
  end

  test "wua: a bad footer is not recognised and never raises" do
    assert_nil analyze("a.wua", wua_image(magic: 0))
    assert_nil analyze("a.wua", wua_image(version: 2))
    assert_nil analyze("a.wua", wua_image(total_size: 5))
    assert_nil analyze("a.wua", "")
    assert_nil analyze("a.wua", "x".b * 100)
    assert_nil analyze("a.wua", "x".b * 144)
    assert_nil analyze("a.wua", ("\0".b * 400))
    truncated = wua_image
    assert_nil analyze("a.wua", truncated.byteslice(0, truncated.bytesize - 50))
    garbage = Random.new(7).bytes(2000)
    assert_nil analyze("a.wua", garbage)
  end

  test "wua: a section pointing outside the file is rejected" do
    image = wua_image
    # The names record is the third {offset, size} pair of the footer.
    footer_start = image.bytesize - 144
    image.setbyte(footer_start + 32 + 7, 0xFF) # names offset low byte
    image.setbyte(footer_start + 32 + 6, 0xFF)
    assert_nil analyze("a.wua", image)
  end

  test "wux: magic, not playable" do
    result = analyze("a.wux", wux_image)
    assert_equal [ "wiiu", "wux", false ], result.metadata.values_at("platform", "format", "playable")
    assert_nil analyze("a.wux", blank_bytes)
  end

  test "wud: product code and region from the header, not playable" do
    result = analyze("a.wud", wud_image)
    assert_equal [ "wiiu", "wud", "WUP-P-ALCP", "EU", false ], result.metadata.values_at("platform", "format", "product_code", "region", "playable")
    assert_equal "US", analyze("a.wud", wud_image(product_code: "WUP-P-ALCE")).metadata["region"]
    assert_equal "JP", analyze("a.wud", wud_image(product_code: "WUP-P-ALCJ")).metadata["region"]
    odd = analyze("a.wud", "WUP-?".b + ("\0" * 20).b)
    assert_equal "wud", odd.metadata["format"]
    assert_not odd.metadata.key?("product_code")
    assert_nil analyze("a.wud", blank_bytes)
    assert_nil analyze("a.wud", "WU")
  end

  test "a matching extension with the wrong content, and other files, stay unknown" do
    assert_nil analyze("a.wua", wux_image)
    assert_nil analyze("a.zip", wua_image)
    assert_nil analyze("a.iso", wua_image)
    assert_nil AssetAnalyzers::WiiUTitle.call(@dir.join("missing.wua").to_s, nil)
  end

  test "for_games runs WiiUTitle after NintendoDisc and before RomCartridge" do
    games = AssetAnalyzers.for_games
    assert_equal [ AssetAnalyzers::NintendoDisc, AssetAnalyzers::WiiUTitle, AssetAnalyzers::RomCartridge ], games
    wua = @dir.join("Nintendo_Land.wua")
    File.binwrite(wua, wua_image)
    assert_equal "wiiu", AssetAnalyzers.analyze_game(wua.to_s, nil).metadata["platform"]
  end

  test "a Wii or GameCube image is still analysed as one" do
    iso = @dir.join("game.iso")
    File.binwrite(iso, iso_image(game_id: "RSPE01", title: "Wii Sports"))
    assert_equal "wii", AssetAnalyzers.analyze_game(iso.to_s, nil).metadata["platform"]
    assert_nil AssetAnalyzers::WiiUTitle.call(iso.to_s, nil)
  end

  private

  def blank_bytes
    ("\0" * 128).b
  end
end
