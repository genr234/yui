require "zlib"

# Builds tiny synthetic disc images (headers only, never real game data) for each
# container format the asset processor understands.
module DiscFixtures
  WII_MAGIC = [ 0x5D1C9EA3 ].pack("N")
  GAMECUBE_MAGIC = [ 0xC2339F3D ].pack("N")

  def disc_header(game_id:, title:, platform: :wii)
    header = ("\0" * 0x440).b
    header[0, 6] = game_id.b
    if platform == :wii
      header[0x18, 4] = WII_MAGIC
    else
      header[0x1C, 4] = GAMECUBE_MAGIC
    end
    header[0x20, title.bytesize] = title.b
    header
  end

  def iso_image(**options)
    disc_header(**options) + ("\0" * 0x1000).b
  end

  def wbfs_image(**options)
    image = ("\0" * 0x200).b
    image[0, 4] = "WBFS"
    image[4, 4] = [ 1024 ].pack("N")
    image[8, 1] = [ 9 ].pack("C") # 512-byte HD sectors
    image[9, 1] = [ 21 ].pack("C")
    image + disc_header(**options) + ("\0" * 0x400).b
  end

  # WIA/RVZ: 0x48-byte header 1, then header 2 with disc_type at 0x48 and the raw
  # disc header at 0x58.
  def wia_image(magic: "RVZ\x01", **options)
    image = ("\0" * 0x100).b
    image[0, 4] = magic.b
    image[4, 4] = [ 0x01000000 ].pack("N")
    image[0x48, 4] = [ options[:platform] == :gamecube ? 1 : 2 ].pack("N")
    image[0x58, 0x80] = disc_header(**options).byteslice(0, 0x80)
    image + ("\0" * 0x200).b
  end

  def gcz_image(**options)
    block = disc_header(**options) + ("\0" * (0x4000 - 0x440)).b
    compressed = Zlib::Deflate.deflate(block)
    header = [ 0xB10BC001, options[:platform] == :gamecube ? 0 : 1, compressed.bytesize, block.bytesize, block.bytesize, 1 ].pack("VVQ<Q<VV")
    header + [ 0 ].pack("Q<") + [ Zlib.adler32(compressed) ].pack("V") + compressed
  end

  def ciso_image(**options)
    image = ("\0" * 0x8000).b
    image[0, 4] = "CISO"
    image[4, 4] = [ 0x200000 ].pack("V")
    image[8, 1] = "\x01".b
    image + disc_header(**options)
  end

  def png_bytes
    "\x89PNG\r\n\x1A\n".b + ("\0" * 32).b
  end

  def write_asset_file(asset, bytes)
    FileUtils.mkdir_p(asset.directory)
    File.binwrite(asset.file_path, bytes)
  end
end
