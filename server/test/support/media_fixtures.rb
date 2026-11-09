# Builds tiny synthetic cartridge headers and audio containers (never real games or
# songs) for the asset analyzers.
module MediaFixtures
  PNG = ("\x89PNG\r\n\x1A\n".b + "cover-pixels".b).freeze
  JPEG = ("\xFF\xD8\xFF\xE0".b + "jpeg-pixels".b).freeze

  def blank(size)
    ("\0" * size).b
  end

  def put(bytes, offset, data)
    bytes[offset, data.bytesize] = data.b
    bytes
  end

  def nes_rom(nes2: false)
    put(blank(64), 0, "NES\x1A\x02\x01").tap { |rom| rom.setbyte(7, 0x08) if nes2 }
  end

  def snes_rom(title: "TEST CART", header_at: 0x7FDC, copier: false, checksum: 0x1234, region: 1)
    rom = blank(0x10200)
    base = (copier ? 512 : 0) + header_at
    put(rom, base - 0x1C, title.ljust(21))
    rom.setbyte(base - 3, region)
    put(rom, base, [ checksum ^ 0xFFFF, checksum ].pack("v2"))
    rom
  end

  def game_boy_rom(title: "TINY LAND", cgb: nil)
    rom = blank(0x200)
    put(rom, 0x134, title)
    rom.setbyte(0x143, cgb) if cgb
    rom.setbyte(0x14D, rom.byteslice(0x134, 0x19).bytes.sum(0) { |byte| -byte - 1 } & 0xFF)
    rom
  end

  def gba_rom(title: "TINYADVENTURE", code: "AXVE")
    rom = blank(0xC0)
    put(rom, 0xA0, title.ljust(12)[0, 12])
    put(rom, 0xAC, code)
    rom.setbyte(0xB2, 0x96)
    rom
  end

  def genesis_rom(title: "TINY BLAST", region: "JUE")
    rom = blank(0x200)
    put(rom, 0x100, "SEGA MEGA DRIVE ")
    put(rom, 0x150, title.ljust(48))
    put(rom, 0x1F0, region)
    rom
  end

  def n64_rom(title: "TINY KART 64", order: "z64")
    rom = blank(0x1000)
    put(rom, 0, "\x80\x37\x12\x40")
    put(rom, 0x20, title.ljust(20))
    put(rom, 0x3E, "E")
    case order
    when "v64" then rom.unpack("v*").pack("n*")
    when "n64" then rom.unpack("N*").pack("V*")
    else rom
    end
  end

  def syncsafe(number)
    [ (number >> 21) & 0x7F, (number >> 14) & 0x7F, (number >> 7) & 0x7F, number & 0x7F ].pack("C4")
  end

  def id3_frame(id, body, version: 4)
    size = version == 4 ? syncsafe(body.bytesize) : [ body.bytesize ].pack("N")
    id.b + size + "\0\0".b + body.b
  end

  def id3_text(id, text, version: 4, encoding: 3)
    id3_frame(id, [ encoding ].pack("C") + text.encode(encoding == 1 ? "UTF-16" : "UTF-8").b, version: version)
  end

  # One 128 kbit/s 44.1 kHz stereo MPEG-1 Layer III frame header; with `frames` it carries a Xing count.
  def mp3_audio(frames: nil)
    frame = blank(417)
    put(frame, 0, "\xFF\xFB\x90\x00")
    put(frame, 36, "Xing" + [ 1 ].pack("N") + [ frames ].pack("N")) if frames
    frame
  end

  def mp3_file(version: 4, frames: nil, picture: PNG, encoding: 3)
    frames_data = id3_text("TIT2", "Tiny Song", version: version, encoding: encoding) +
      id3_text("TPE1", "The Fixtures", version: version, encoding: encoding) +
      id3_text("TALB", "Synthetic", version: version, encoding: encoding) +
      id3_text("TRCK", "3/12", version: version, encoding: encoding)
    if picture
      frames_data += id3_frame("APIC", "\x00".b + "image/png\0".b + "\x03".b + "front\0".b + picture, version: version)
    end
    header = "ID3".b + [ version, 0, 0 ].pack("C3") + syncsafe(frames_data.bytesize)
    header + frames_data + mp3_audio(frames: frames)
  end

  def vorbis_comment_block(comments, vendor: "fixture")
    body = [ vendor.bytesize ].pack("V") + vendor + [ comments.size ].pack("V")
    comments.each { |key, value| body += [ "#{key}=#{value}".bytesize ].pack("V") + "#{key}=#{value}" }
    body.b
  end

  def flac_block(type, data, last: false)
    [ (last ? 0x80 : 0) | type ].pack("C") + [ data.bytesize ].pack("N")[1, 3] + data
  end

  def flac_file(picture: PNG, rate: 44_100, samples: 44_100 * 90)
    info = blank(34)
    packed = (rate << 44) | (1 << 41) | (15 << 36) | samples # rate:20, channels-1:3, bps-1:5, samples:36
    put(info, 10, [ packed ].pack("Q>"))
    pic = [ 3, 9 ].pack("N2") + "image/png" + [ 0 ].pack("N") + [ 1, 1, 24, 0, picture.bytesize ].pack("N5") + picture
    comments = vorbis_comment_block({ "TITLE" => "Flac Tune", "artist" => "The Fixtures", "ALBUM" => "Synthetic", "TRACKNUMBER" => "7" })
    "fLaC".b + flac_block(0, info) + flac_block(4, comments) + flac_block(6, pic, last: true)
  end

  def ogg_page(packet_bytes, granule: 0, sequence: 0, header_type: 0)
    segments = [ 255 ] * (packet_bytes.bytesize / 255) + [ packet_bytes.bytesize % 255 ]
    "OggS".b + [ 0, header_type, granule, 1, sequence, 0, segments.size ].pack("CCq<VVVC") + segments.pack("C*") + packet_bytes
  end

  def ogg_file(seconds: 12, rate: 48_000, picture: nil)
    ident = "\x01vorbis".b + [ 0, 2, rate, 0, 0, 0 ].pack("VCVVVV") + "\xB8\x01".b
    comments = { "TITLE" => "Ogg Tune", "ARTIST" => "The Fixtures", "ALBUM" => "Synthetic", "TRACKNUMBER" => "2" }
    comments["METADATA_BLOCK_PICTURE"] = Base64.strict_encode64(flac_picture_block(picture)) if picture
    comment = "\x03vorbis".b + vorbis_comment_block(comments) + "\x01".b
    ogg_page(ident, header_type: 2) + ogg_page(comment, sequence: 1) + ogg_page("audio".b, granule: seconds * rate, sequence: 2, header_type: 4)
  end

  def flac_picture_block(picture)
    [ 3, 9 ].pack("N2") + "image/png" + [ 0 ].pack("N") + [ 1, 1, 24, 0, picture.bytesize ].pack("N5") + picture
  end

  def mp4_box(type, body)
    [ body.bytesize + 8 ].pack("N") + type.b + body.b
  end

  def mp4_data(payload, flags: 1)
    mp4_box("data", [ flags ].pack("N") + [ 0 ].pack("N") + payload)
  end

  def m4a_file(picture: JPEG)
    items = mp4_box("\xA9nam".b, mp4_data("Mp4 Tune")) + mp4_box("\xA9ART".b, mp4_data("The Fixtures")) +
      mp4_box("\xA9alb".b, mp4_data("Synthetic")) + mp4_box("trkn", mp4_data([ 0, 5, 9, 0 ].pack("n4"), flags: 0))
    items += mp4_box("covr", mp4_data(picture, flags: 13)) if picture
    mvhd = mp4_box("mvhd", [ 0, 0, 0, 1000, 200_000 ].pack("N5") + blank(80))
    meta = mp4_box("meta", [ 0 ].pack("N") + mp4_box("ilst", items))
    mp4_box("ftyp", "M4A \0\0\0\0M4A ") + mp4_box("moov", mvhd + mp4_box("udta", meta)) + mp4_box("mdat", "audio")
  end
end
