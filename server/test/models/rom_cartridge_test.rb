require "test_helper"

class RomCartridgeTest < ActiveSupport::TestCase
  include MediaFixtures

  setup { @dir = Pathname(Dir.mktmpdir("roms")) }
  teardown { FileUtils.rm_rf(@dir) }

  def analyze(name, bytes)
    path = @dir.join(name)
    File.binwrite(path, bytes)
    AssetAnalyzers::RomCartridge.call(path.to_s, nil)
  end

  test "nes: iNES and NES 2.0 magic, title from the file name" do
    result = analyze("Tiny_Quest.nes", nes_rom)
    assert_equal "nes", result.metadata["platform"]
    assert_equal "ines", result.metadata["format"]
    assert_equal "header", result.metadata["confidence"]
    assert_equal "Tiny Quest", result.metadata["title"]
    assert_nil result.title
    assert_match(/\Ahsl\(\d+ 55% 45%\)\z/, result.metadata["cover_color"])
    assert_equal "nes2", analyze("a.nes", nes_rom(nes2: true)).metadata["format"]
    assert_nil analyze("bad.nes", blank(64))
  end

  test "cover colour is stable per title" do
    assert_equal analyze("a.nes", nes_rom).metadata["cover_color"], analyze("a.nes", nes_rom).metadata["cover_color"]
    assert_not_equal analyze("aaa.nes", nes_rom).metadata["cover_color"], analyze("zzzzzz.nes", nes_rom).metadata["cover_color"]
  end

  test "snes: checksum complement at lorom, hirom and behind a copier header" do
    lo = analyze("x.sfc", snes_rom)
    assert_equal [ "snes", "lorom", "TEST CART", "US" ], lo.metadata.values_at("platform", "format", "title", "region")
    assert_equal "TEST CART", lo.title
    assert_equal "hirom", analyze("x.sfc", snes_rom(header_at: 0xFFDC)).metadata["format"]
    assert_equal "TEST CART", analyze("x.smc", snes_rom(copier: true)).metadata["title"]
    assert_nil analyze("x.sfc", blank(0x10200))
    broken = snes_rom
    broken.setbyte(0x7FDC, broken.getbyte(0x7FDC) ^ 1)
    assert_nil analyze("x.sfc", broken)
  end

  test "game boy: header checksum, CGB flag picks gbc" do
    gb = analyze("t.gb", game_boy_rom)
    assert_equal [ "gb", "TINY LAND" ], gb.metadata.values_at("platform", "title")
    assert_equal "gbc", analyze("t.gbc", game_boy_rom(cgb: 0x80)).metadata["platform"]
    bad = game_boy_rom
    bad.setbyte(0x14D, bad.getbyte(0x14D) ^ 0xFF)
    assert_nil analyze("t.gb", bad)
  end

  test "gba: 0x96 at 0xB2, region from the game code" do
    result = analyze("t.gba", gba_rom)
    assert_equal [ "gba", "TINYADVENTUR", "US" ], result.metadata.values_at("platform", "title", "region")
    rom = gba_rom
    rom.setbyte(0xB2, 0)
    assert_nil analyze("t.gba", rom)
  end

  test "genesis: SEGA at 0x100" do
    result = analyze("t.md", genesis_rom)
    assert_equal [ "genesis", "TINY BLAST", "JP" ], result.metadata.values_at("platform", "title", "region")
    assert_nil analyze("t.md", blank(0x200))
  end

  test "n64: all three byte orders give the same title" do
    %w[z64 v64 n64].each do |order|
      result = analyze("t.#{order}", n64_rom(order: order))
      assert_equal [ "n64", order, "TINY KART 64", "US" ], result.metadata.values_at("platform", "format", "title", "region"), order
    end
    assert_nil analyze("t.z64", blank(0x1000))
  end

  test "extension-only platforms" do
    { "a.sms" => "sms", "a.gg" => "gg", "a.pce" => "pce", "a.a26" => "atari2600", "a.lnx" => "lynx" }.each do |name, platform|
      result = analyze(name, "anything".b)
      assert_equal platform, result.metadata["platform"], name
      assert_equal "extension", result.metadata["confidence"]
    end
  end

  test "zips, unknown extensions and garbage are not recognised" do
    assert_nil analyze("games.zip", "PK\x03\x04".b + blank(100))
    assert_nil analyze("a.txt", nes_rom)
    assert_nil analyze("a.gba", "".b)
    assert_nil analyze("a.sfc", "\xFF".b * 10)
    assert_nil analyze("a.n64", "\x80\x37".b)
    assert_nil AssetAnalyzers::RomCartridge.call(@dir.join("missing.nes").to_s, nil)
  end

  test "the registry tries disc images first and falls through to cartridges" do
    path = @dir.join("t.gb")
    File.binwrite(path, game_boy_rom)
    assert_equal "gb", AssetAnalyzers.analyze_game(path.to_s, nil).metadata["platform"]
  end
end
