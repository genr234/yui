# Cartridge dumps: sets metadata.platform to nes, snes, gb, gbc, gba, genesis, n64, sms,
# gg, pce, atari2600 or lynx. The first five-or-so need the extension and a header check
# ("confidence" => "header"); the rest are recognised by extension only
# ("confidence" => "extension"). Archives are never opened. No box art is fetched; the
# result carries a stable `cover_color` instead.
module AssetAnalyzers
  module RomCartridge
    HEAD_BYTES = 0x10200 # enough for the SNES header at 0xFFDC plus a 512-byte copier header

    HEADER_PLATFORMS = {
      "nes" => "nes", "sfc" => "snes", "smc" => "snes", "gb" => "gb", "gbc" => "gbc", "gba" => "gba",
      "md" => "genesis", "gen" => "genesis", "smd" => "genesis",
      "n64" => "n64", "z64" => "n64", "v64" => "n64"
    }.freeze
    EXTENSION_PLATFORMS = {
      "sms" => "sms", "gg" => "gg", "pce" => "pce", "a26" => "atari2600", "lnx" => "lynx"
    }.freeze

    REGIONS = { "J" => "JP", "E" => "US", "U" => "US", "P" => "EU", "D" => "EU", "F" => "EU", "I" => "EU", "S" => "EU" }.freeze
    N64_MAGICS = { "\x80\x37\x12\x40".b => "z64", "\x37\x80\x40\x12".b => "v64", "\x40\x12\x37\x80".b => "n64" }.freeze

    def self.call(path, _base_url = nil)
      extension = File.extname(path.to_s).delete(".").downcase
      return unless HEADER_PLATFORMS.key?(extension) || EXTENSION_PLATFORMS.key?(extension)

      found = if EXTENSION_PLATFORMS.key?(extension)
        { platform: EXTENSION_PLATFORMS.fetch(extension), format: extension, confidence: "extension" }
      else
        head = File.binread(path, HEAD_BYTES)
        head && detect(extension, head.b)
      end
      return if found.nil?

      build_result(path, found)
    rescue SystemCallError, EncodingError
      nil
    end

    def self.build_result(path, found)
      fallback = Asset.default_title(File.basename(path.to_s))
      title = found[:title] || fallback
      Result.new(
        title: found[:title],
        metadata: {
          "platform" => found[:platform],
          "format" => found[:format],
          "confidence" => found.fetch(:confidence, "header"),
          "title" => title,
          "region" => found[:region],
          "cover_color" => cover_color(title)
        }
      )
    end

    # A stable hue per title, so a game keeps its colour on every kiosk.
    def self.cover_color(title)
      hue = Digest::SHA256.hexdigest(title.to_s.downcase)[0, 8].to_i(16) % 360
      "hsl(#{hue} 55% 45%)"
    end

    def self.detect(extension, head)
      case HEADER_PLATFORMS.fetch(extension)
      when "nes" then nes(head)
      when "snes" then snes(head)
      when "gb", "gbc" then game_boy(head, extension)
      when "gba" then gba(head)
      when "genesis" then genesis(head)
      when "n64" then n64(head)
      end
    end

    def self.clean(bytes)
      text = bytes.to_s.b.split("\0".b, 2).first.to_s.tr("^\x20-\x7E".b, " ").squeeze(" ").strip
      text.presence&.force_encoding(Encoding::UTF_8)
    end

    def self.nes(head)
      return unless head.byteslice(0, 4) == "NES\x1A".b

      { platform: "nes", format: head.getbyte(7).to_i & 0x0C == 0x08 ? "nes2" : "ines" }
    end

    # The header checksum and its complement add up to 0xFFFF. Dumps with a copier header
    # have the real image 512 bytes in.
    def self.snes(head)
      [ 0, 512 ].each do |skip|
        [ [ 0x7FDC, "lorom" ], [ 0xFFDC, "hirom" ] ].each do |offset, layout|
          at = skip + offset
          next if head.bytesize < at + 4

          complement, checksum = head.byteslice(at, 4).unpack("v2")
          next unless complement + checksum == 0xFFFF && !(complement.zero? && checksum.zero?)

          title = clean(head.byteslice(at - 0x1C, 21))
          region = { 0 => "JP", 1 => "US", 2 => "EU" }[head.getbyte(at - 3)]
          return { platform: "snes", format: layout, title: title, region: region }
        end
      end
      nil
    end

    def self.game_boy(head, extension)
      return if head.bytesize < 0x150

      sum = head.byteslice(0x134, 0x19).bytes.sum(0) { |byte| -byte - 1 } & 0xFF
      return unless sum == head.getbyte(0x14D)

      cgb = [ 0x80, 0xC0 ].include?(head.getbyte(0x143))
      title = clean(head.byteslice(0x134, cgb ? 15 : 16))
      { platform: cgb ? "gbc" : "gb", format: extension, title: title, region: head.getbyte(0x14A) == 0 ? "JP" : nil }
    end

    def self.gba(head)
      return if head.bytesize < 0xC0 || head.getbyte(0xB2) != 0x96

      { platform: "gba", format: "gba", title: clean(head.byteslice(0xA0, 12)), region: REGIONS[head.byteslice(0xAF, 1)] }
    end

    def self.genesis(head)
      return if head.bytesize < 0x200 || head.byteslice(0x100, 4) != "SEGA".b

      title = clean(head.byteslice(0x150, 48)) || clean(head.byteslice(0x120, 48))
      { platform: "genesis", format: "genesis", title: title, region: REGIONS[head.byteslice(0x1F0, 1)] }
    end

    # Cartridge dumps come big-endian (z64), 16-bit swapped (v64) or word swapped (n64).
    def self.n64(head)
      return if head.bytesize < 0x40

      order = N64_MAGICS[head.byteslice(0, 4)]
      return if order.nil?

      header = head.byteslice(0, 0x40)
      header = case order
      when "v64" then header.unpack("v*").pack("n*")
      when "n64" then header.unpack("V*").pack("N*")
      else header
      end
      { platform: "n64", format: order, title: clean(header.byteslice(0x20, 20)), region: REGIONS[header.byteslice(0x3E, 1)] }
    end
  end
end
