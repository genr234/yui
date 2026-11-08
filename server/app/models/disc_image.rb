require "zlib"

# Reads the Wii / GameCube disc header from a disc image without decompressing the
# whole file. Supported containers and where their 0x80-byte disc header lives:
#
#   iso / gcm  raw disc, header at 0x0
#   wbfs       "WBFS" magic, header at the first HD sector (1 << byte 8, normally 0x200)
#   wia / rvz  "WIA\x01" / "RVZ\x01" magic; WIAHeader1 is 0x48 bytes, then WIAHeader2 starts
#              with disc_type (u32 BE, 1 = GameCube, 2 = Wii), compression_type,
#              compression_level and chunk_size, followed by the uncompressed
#              disc_header[0x80] at file offset 0x58 (Dolphin Source/Core/DiscIO/WIABlob.h)
#   gcz        magic 0xB10BC001 (LE); header at the start of block 0, zlib compressed
#              unless the top bit of the block pointer is set (Dolphin CompressedBlob.h)
#   ciso       "CISO" magic, 0x8000-byte header with a block map; block 0's data at 0x8000
#
# Disc header fields: game ID at 0x0 (6 bytes), Wii magic 0x5D1C9EA3 at 0x18,
# GameCube magic 0xC2339F3D at 0x1C, title at 0x20 (NUL terminated).
class DiscImage
  WII_MAGIC = 0x5D1C9EA3
  GAMECUBE_MAGIC = 0xC2339F3D
  GCZ_MAGIC = 0xB10BC001
  WIA_DISC_HEADER_OFFSET = 0x58
  WIA_DISC_TYPE_OFFSET = 0x48
  CISO_HEADER_SIZE = 0x8000
  HEADER_SIZE = 0x80
  GAME_ID_FORMAT = /\A[A-Z0-9]{6}\z/

  # Region from the 4th character of the game ID.
  REGIONS = {
    "E" => "US", "N" => "US",
    "J" => "JP", "K" => "KR", "W" => "TW", "C" => "CN", "Q" => "KR", "T" => "KR",
    "P" => "EU", "D" => "EU", "F" => "EU", "I" => "EU", "S" => "EU", "H" => "EU",
    "X" => "EU", "Y" => "EU", "Z" => "EU", "L" => "EU", "M" => "EU", "V" => "EU",
    "U" => "AU", "R" => "RU"
  }.freeze

  Result = Data.define(:format, :game_id, :platform, :disc_title) do
    def region
      DiscImage.region_for(game_id)
    end

    def detected?
      game_id.present?
    end
  end

  UNKNOWN = Result.new(format: "unknown", game_id: nil, platform: nil, disc_title: nil)

  def self.read(path)
    new(path).read
  end

  def self.region_for(game_id)
    REGIONS[game_id.to_s[3]] if game_id.present?
  end

  def initialize(path)
    @path = path
  end

  def read
    File.open(@path, "rb") do |file|
      @file = file
      detect || UNKNOWN
    end
  rescue Errno::ENOENT, Errno::EISDIR, Zlib::Error
    UNKNOWN
  end

  private

  def detect
    magic = read_at(0, 4)
    return if magic.nil? || magic.bytesize < 4

    case magic
    when "WBFS" then wbfs
    when "WIA\x01".b then wia("wia")
    when "RVZ\x01".b then wia("rvz")
    when "CISO" then ciso
    else
      if magic.unpack1("V") == GCZ_MAGIC
        gcz
      else
        from_disc_header(read_at(0, HEADER_SIZE), "iso")
      end
    end
  end

  def wbfs
    shift = read_at(8, 1)&.unpack1("C").to_i
    sector_size = shift.between?(9, 12) ? 1 << shift : 0x200
    from_disc_header(read_at(sector_size, HEADER_SIZE), "wbfs")
  end

  def wia(format)
    disc_type = read_at(WIA_DISC_TYPE_OFFSET, 4)&.unpack1("N")
    result = from_disc_header(read_at(WIA_DISC_HEADER_OFFSET, HEADER_SIZE), format)
    return result if result

    header = read_at(WIA_DISC_HEADER_OFFSET, HEADER_SIZE)
    platform = { 1 => "gamecube", 2 => "wii" }[disc_type]
    game_id = header && header.byteslice(0, 6)
    return unless platform && game_id&.match?(GAME_ID_FORMAT)

    Result.new(format: format, game_id: game_id, platform: platform, disc_title: title_from(header))
  end

  def ciso
    map = read_at(8, 1)
    return unless map == "\x01".b

    from_disc_header(read_at(CISO_HEADER_SIZE, HEADER_SIZE), "ciso")
  end

  def gcz
    header = read_at(0, 32)
    return if header.nil? || header.bytesize < 32

    _magic, _sub_type, compressed_size, _data_size, _block_size, block_count = header.unpack("VVQ<Q<VV")
    return if block_count.zero? || block_count > 50_000_000

    pointers = read_at(32, 16)&.unpack("Q<*") || []
    first = pointers[0]
    return if first.nil?

    data_offset = 32 + block_count * 12
    uncompressed = first[63] == 1
    start = first & ~(1 << 63)
    finish = block_count > 1 && pointers[1] ? (pointers[1] & ~(1 << 63)) : compressed_size
    length = (finish - start).clamp(0, 1.megabyte)
    raw = read_at(data_offset + start, length)
    return if raw.nil?

    block = uncompressed ? raw : Zlib::Inflate.new.inflate(raw)
    from_disc_header(block.byteslice(0, HEADER_SIZE), "gcz")
  end

  def from_disc_header(header, format)
    return if header.nil? || header.bytesize < 0x40

    wii = header.byteslice(0x18, 4).unpack1("N") == WII_MAGIC
    gamecube = header.byteslice(0x1C, 4).unpack1("N") == GAMECUBE_MAGIC
    return unless wii || gamecube

    game_id = header.byteslice(0, 6)
    return unless game_id.match?(GAME_ID_FORMAT)

    Result.new(format: format, game_id: game_id, platform: wii ? "wii" : "gamecube", disc_title: title_from(header))
  end

  def title_from(header)
    raw = header.byteslice(0x20, HEADER_SIZE - 0x20).to_s.split("\x00".b, 2).first.to_s
    # Titles are Shift-JIS on Japanese discs and Latin-1/ASCII elsewhere.
    encoding = header.byteslice(3, 1) == "J" ? Encoding::Shift_JIS : Encoding::ISO_8859_1
    text = raw.dup.force_encoding(encoding)
    text.encode(Encoding::UTF_8, invalid: :replace, undef: :replace).strip.presence
  end

  def read_at(offset, length)
    @file.seek(offset)
    @file.read(length)
  rescue Errno::EINVAL
    nil
  end
end
