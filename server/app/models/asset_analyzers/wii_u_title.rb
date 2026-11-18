# Wii U titles: sets metadata.platform to "wiiu" and metadata.format to "wua", "wux" or "wud".
# Only WUA (a ZArchive holding the decrypted base game, update and DLC) is playable
# ("playable" => true); WUX and WUD are recognised so they can be labelled "convert to WUA".
# Headers only: nothing is fetched, nothing is decompressed, no keys are read or needed, and an
# odd or short file simply is not recognised (the analyzer never raises).
#
# ZArchive layout, checked against github.com/Exzap/ZArchive at commit
# 965b66c8d67b6b7e30fd63b3b75aa91a99ff303b (include/zarchive/zarchivecommon.h, src/zarchivereader.cpp,
# src/zarchivewriter.cpp). Everything is big-endian.
#   Footer, the last 144 bytes: six {offset u64, size u64} records in this order (compressed data,
#   offset records, names, file tree, meta directory, meta data), a 32-byte SHA-256, total size u64,
#   version u32 0x61bf3a01, magic u32 0x169f52d6. The reader requires total size == file size and
#   every section inside the file.
#   Name table (uncompressed): each name is a length prefix then bytes. The prefix byte holds the
#   low 7 bits of the length; when its top bit is set a second byte holds the upper bits
#   (length = (b0 & 0x7F) | (b1 << 7), as the writer emits; the reader has an apparent typo there).
#   File tree (uncompressed): 16-byte entries of four u32. Word 0: top bit 1 = file, low 31 bits =
#   name offset. Directory entries: word 1 = index of the first child, word 2 = child count. Entry 0
#   is the nameless root directory; a directory's children are contiguous.
#   A WUA's root holds one directory per title, named "<title id, 16 hex digits>_v<version>"
#   (Cemu's title manager writes these). Base game ids start with 00050000, updates 0005000e,
#   DLC 0005000c.
module AssetAnalyzers
  module WiiUTitle
    ZARCHIVE_FOOTER_SIZE = 144
    ZARCHIVE_MAGIC = 0x169F52D6
    ZARCHIVE_VERSION_1 = 0x61BF3A01
    ENTRY_SIZE = 16
    MAX_NAME_TABLE = 32 * 1024 * 1024
    MAX_FILE_TREE = 64 * 1024 * 1024
    TITLE_DIRECTORY = /\A([0-9a-f]{16})_v\d+\z/
    BASE_GAME_HIGH_WORD = "00050000"
    WUD_PRODUCT_CODE = /\AWUP-[A-Z0-9]-[A-Z0-9]{4}\z/
    REGIONS = { "J" => "JP", "E" => "US", "U" => "US", "P" => "EU", "K" => "KR" }.freeze

    def self.call(path, _base_url = nil)
      found = case File.extname(path.to_s).delete(".").downcase
      when "wua" then wua(path)
      when "wux" then wux(path)
      when "wud" then wud(path)
      end
      found && build_result(path, found)
    rescue StandardError
      nil
    end

    def self.build_result(path, found)
      title = Asset.default_title(File.basename(path.to_s))
      format = found.fetch(:format)
      Result.new(
        title: nil,
        metadata: {
          "platform" => "wiiu",
          "format" => format,
          "title_id" => found[:title_id],
          "product_code" => found[:product_code],
          "region" => found[:region],
          "playable" => format == "wua",
          "title" => title,
          "cover_color" => RomCartridge.cover_color(title)
        }.compact
      )
    end

    def self.wux(path)
      { format: "wux" } if File.binread(path, 4) == "WUX0".b
    end

    # A raw disc image starts with the product code, e.g. "WUP-P-ALCP" (the last letter is the region).
    def self.wud(path)
      head = File.binread(path, 10)
      return if head.nil? || head.byteslice(0, 4) != "WUP-".b

      code = head.force_encoding(Encoding::US_ASCII)
      return { format: "wud" } unless code.valid_encoding? && code.match?(WUD_PRODUCT_CODE)

      { format: "wud", product_code: code, region: REGIONS[code[-1]] }
    end

    def self.wua(path)
      File.open(path, "rb") do |file|
        size = file.size
        return if size <= ZARCHIVE_FOOTER_SIZE

        sections = read_footer(file, size)
        return if sections.nil?

        names = read_section(file, sections.fetch(:names), MAX_NAME_TABLE)
        tree = read_section(file, sections.fetch(:tree), MAX_FILE_TREE)
        return if names.nil? || tree.nil? || tree.bytesize < ENTRY_SIZE || tree.bytesize % ENTRY_SIZE != 0

        { format: "wua", title_id: base_title_id(names, tree) }
      end
    end

    # { names: [offset, size], tree: [offset, size] }, or nil when this is not a version 1 ZArchive.
    def self.read_footer(file, size)
      file.seek(size - ZARCHIVE_FOOTER_SIZE)
      footer = file.read(ZARCHIVE_FOOTER_SIZE)
      return if footer.nil? || footer.bytesize != ZARCHIVE_FOOTER_SIZE

      records = footer.unpack("Q>12")
      total_size, version, magic = footer.byteslice(128, 16).unpack("Q>L>L>")
      return unless magic == ZARCHIVE_MAGIC && version == ZARCHIVE_VERSION_1 && total_size == size

      sections = records.each_slice(2).to_a
      return unless sections.all? { |offset, length| offset + length <= size }

      { names: sections[2], tree: sections[3] }
    end

    def self.read_section(file, section, limit)
      offset, length = section
      return if length > limit

      file.seek(offset)
      data = file.read(length)
      data if data && data.bytesize == length
    end

    # The id of the first base game directory in the root ("00050000...."), else nil.
    def self.base_title_id(names, tree)
      name_and_flag, first, count = tree.unpack("L>3")
      return if name_and_flag & 0x80000000 != 0 # the root must be a directory

      entries = tree.bytesize / ENTRY_SIZE
      return if first + count > entries

      ids = (first...(first + count)).filter_map do |index|
        flag = tree.byteslice(index * ENTRY_SIZE, 4).unpack1("L>")
        next if flag & 0x80000000 != 0 # a file, not a title directory

        match = TITLE_DIRECTORY.match(node_name(names, flag & 0x7FFFFFFF).to_s)
        match && match[1]
      end
      ids.find { |id| id.start_with?(BASE_GAME_HIGH_WORD) }
    end

    # The name at `offset` in the name table, or nil when the table is too short.
    def self.node_name(names, offset)
      return if offset >= names.bytesize

      length = names.getbyte(offset) & 0x7F
      start = offset + 1
      if names.getbyte(offset) & 0x80 != 0
        return if offset + 1 >= names.bytesize

        length |= names.getbyte(offset + 1) << 7
        start = offset + 2
      end
      return if start + length > names.bytesize

      names.byteslice(start, length).b
    end
  end
end
