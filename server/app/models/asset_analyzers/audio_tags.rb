# Audio files (`media` assets): tags and duration from mp3 (ID3v2.3/2.4), FLAC, Ogg
# Vorbis and m4a/mp4, in plain Ruby. Sets metadata.kind to "audio" with title, artist,
# album, track, duration_ms and, when the file carries art, a cover (cover_key and,
# given a base URL, cover_url). Anything truncated or garbled yields nil, never an error.
module AssetAnalyzers
  module AudioTags
    EXTENSIONS = %w[mp3 flac ogg oga m4a m4b mp4].freeze
    MAX_TAG_BYTES = 16.megabytes

    Tags = Struct.new(:title, :artist, :album, :track, :duration_ms, :picture, keyword_init: true)

    def self.call(path, base_url = nil)
      extension = File.extname(path.to_s).delete(".").downcase
      return unless EXTENSIONS.include?(extension)

      tags = File.open(path, "rb") do |file|
        case extension
        when "mp3" then Mp3.read(file)
        when "flac" then Flac.read(file)
        when "ogg", "oga" then Ogg.read(file)
        else Mp4.read(file)
        end
      end
      build_result(tags, base_url)
    rescue StandardError => error
      Rails.logger.info("[AssetAnalyzers::AudioTags] #{error.class}: #{error.message}")
      nil
    end

    def self.build_result(tags, base_url)
      return if tags.nil? || tags.to_h.values.all?(&:nil?)

      cover_key = tags.picture && MediaCover.store(tags.picture)
      metadata = {
        "kind" => "audio",
        "title" => tags.title, "artist" => tags.artist, "album" => tags.album,
        "track" => tags.track, "duration_ms" => tags.duration_ms,
        "cover_key" => cover_key,
        "cover_url" => (MediaCover.url_for(cover_key, base_url) if cover_key && base_url.present?)
      }.compact
      Result.new(title: tags.title, metadata: metadata)
    end

    def self.text(value)
      value.to_s.dup.force_encoding(Encoding::UTF_8).scrub("").delete("\0").strip.presence&.truncate(200)
    end

    def self.track_number(value)
      number = value.to_s[/\A\s*(\d+)/, 1]&.to_i
      number if number&.positive?
    end

    # Vorbis comments (FLAC and Ogg): returns { "TITLE" => "…" } with upper-cased keys.
    def self.vorbis_comments(data, offset = 0)
      vendor = data.byteslice(offset, 4)&.unpack1("V")
      return {} if vendor.nil?

      offset += 4 + vendor
      count = data.byteslice(offset, 4)&.unpack1("V")
      return {} if count.nil?

      offset += 4
      comments = {}
      count.clamp(0, 4096).times do
        length = data.byteslice(offset, 4)&.unpack1("V")
        break if length.nil? || offset + 4 + length > data.bytesize

        key, value = data.byteslice(offset + 4, length).split("=", 2)
        comments[key.to_s.upcase] ||= value if value
        offset += 4 + length
      end
      comments
    end

    def self.tags_from_comments(comments, duration_ms, picture)
      Tags.new(
        title: text(comments["TITLE"]), artist: text(comments["ARTIST"]), album: text(comments["ALBUM"]),
        track: track_number(comments["TRACKNUMBER"]), duration_ms: duration_ms, picture: picture
      )
    end

    module Mp3
      V1_BITRATES = [ 0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320 ].freeze
      V2_BITRATES = [ 0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160 ].freeze
      RATES = { 3 => [ 44100, 48000, 32000 ], 2 => [ 22050, 24000, 16000 ], 0 => [ 11025, 12000, 8000 ] }.freeze
      TEXT_FRAMES = { "TIT2" => :title, "TPE1" => :artist, "TALB" => :album, "TRCK" => :track, "TLEN" => :length }.freeze

      def self.read(file)
        tags = Tags.new
        header = file.read(10)
        audio_start = 0
        if header && header.bytesize == 10 && header.start_with?("ID3") && [ 3, 4 ].include?(header.getbyte(3))
          size = syncsafe(header.byteslice(6, 4))
          return nil if size > MAX_TAG_BYTES

          body = file.read(size).to_s.b
          return nil if body.bytesize < size

          frames(tags, body, header.getbyte(3), header.getbyte(5))
          audio_start = 10 + size
        end
        tags.duration_ms ||= duration(file, audio_start)
        tags
      end

      def self.syncsafe(bytes)
        bytes.unpack("C4").inject(0) { |sum, byte| (sum << 7) | (byte & 0x7F) }
      end

      def self.frames(tags, body, version, flags)
        body = body.gsub("\xFF\x00".b, "\xFF".b) if flags & 0x80 != 0
        offset = 0
        if flags & 0x40 != 0 && body.bytesize >= 4
          offset = version == 4 ? syncsafe(body.byteslice(0, 4)) : body.unpack1("N") + 4
        end
        while offset + 10 <= body.bytesize
          id = body.byteslice(offset, 4)
          break unless id.match?(/\A[A-Z0-9]{4}\z/n)

          raw = body.byteslice(offset + 4, 4)
          size = version == 4 ? syncsafe(raw) : raw.unpack1("N")
          data = body.byteslice(offset + 10, size)
          break if data.nil? || data.bytesize < size

          frame(tags, id, data)
          offset += 10 + size
        end
      end

      def self.frame(tags, id, data)
        if (field = TEXT_FRAMES[id])
          value = decode(data.byteslice(0, 1).to_s.unpack1("C").to_i, data.byteslice(1..).to_s)
          case field
          when :track then tags.track ||= AudioTags.track_number(value)
          when :length then tags.duration_ms ||= value.to_i if value.to_i.positive?
          else tags[field] ||= AudioTags.text(value)
          end
        elsif id == "APIC" && tags.picture.nil?
          tags.picture = picture(data)
        end
      end

      # APIC: encoding, MIME (latin-1, NUL), picture type, description (encoded, NUL), data.
      def self.picture(data)
        encoding = data.getbyte(0).to_i
        mime_end = data.index("\0".b, 1)
        return nil if mime_end.nil?

        cursor = mime_end + 2 # skip the NUL and the picture type
        step = [ 1, 2 ].include?(encoding) ? 2 : 1
        cursor += step while cursor + step <= data.bytesize && data.byteslice(cursor, step) != "\0".b * step
        data.byteslice((cursor + step)..)
      end

      def self.decode(encoding, bytes)
        bytes = bytes.b
        text = case encoding
        when 0 then bytes.force_encoding(Encoding::ISO_8859_1).encode(Encoding::UTF_8)
        when 1 then bytes.force_encoding(Encoding::UTF_16).encode(Encoding::UTF_8, invalid: :replace, undef: :replace)
        when 2 then bytes.force_encoding(Encoding::UTF_16BE).encode(Encoding::UTF_8, invalid: :replace, undef: :replace)
        else bytes.force_encoding(Encoding::UTF_8)
        end
        text.split("\0").first.to_s
      rescue EncodingError
        ""
      end

      # Xing/Info frame count when there is one, else a constant-bitrate estimate.
      def self.duration(file, audio_start)
        file.seek(audio_start)
        chunk = file.read(64.kilobytes).to_s.b
        sync = chunk.index(/\xFF[\xE2-\xFF]/n)
        return nil if sync.nil?

        head = chunk.byteslice(sync, 4).unpack1("N")
        version = (head >> 19) & 3
        return nil if version == 1 || (head >> 17) & 3 != 1 # reserved, or not Layer III

        bitrate = (version == 3 ? V1_BITRATES : V2_BITRATES)[(head >> 12) & 15]
        rate = RATES[version][(head >> 10) & 3]
        return nil if bitrate.nil? || bitrate.zero? || rate.nil?

        samples = version == 3 ? 1152 : 576
        mono = (head >> 6) & 3 == 3
        xing = sync + 4 + (version == 3 ? (mono ? 17 : 32) : (mono ? 9 : 17))
        if %w[Xing Info].include?(chunk.byteslice(xing, 4)) && chunk.getbyte(xing + 7).to_i & 1 == 1
          frames = chunk.byteslice(xing + 8, 4).unpack1("N")
          return (frames * samples * 1000.0 / rate).round
        end
        ((file.size - audio_start - sync) * 8.0 / bitrate).round # bitrate is kbit/s, so bits / kbit/s = ms
      end
    end

    module Flac
      def self.read(file)
        return nil unless file.read(4) == "fLaC"

        comments = {}
        picture = nil
        duration = nil
        loop do
          header = file.read(4)
          break if header.nil? || header.bytesize < 4

          last = header.getbyte(0) & 0x80 != 0
          type = header.getbyte(0) & 0x7F
          length = ("\0" + header.byteslice(1, 3)).unpack1("N")
          if (type == 0 && length >= 18) || (type == 4 && length <= MAX_TAG_BYTES) || (type == 6 && length <= MAX_TAG_BYTES)
            data = file.read(length)
            break if data.nil? || data.bytesize < length

            case type
            when 0 then duration = stream_duration(data)
            when 4 then comments = AudioTags.vorbis_comments(data)
            when 6 then picture ||= picture_data(data)
            end
          else
            file.seek(length, IO::SEEK_CUR)
          end
          break if last
        end
        AudioTags.tags_from_comments(comments, duration, picture)
      end

      def self.stream_duration(data)
        rate = (data.getbyte(10) << 12) | (data.getbyte(11) << 4) | (data.getbyte(12) >> 4)
        total = ((data.getbyte(13) & 0x0F) << 32) | data.byteslice(14, 4).unpack1("N")
        (total * 1000.0 / rate).round if rate.positive? && total.positive?
      end

      # PICTURE: type, MIME, description, width, height, depth, colours, data length, data.
      def self.picture_data(data)
        offset = 4
        mime = data.byteslice(offset, 4).unpack1("N")
        offset += 4 + mime
        desc = data.byteslice(offset, 4).unpack1("N")
        offset += 4 + desc + 16
        size = data.byteslice(offset, 4).unpack1("N")
        data.byteslice(offset + 4, size)
      end
    end

    module Ogg
      def self.read(file)
        head = file.read(MAX_TAG_BYTES).to_s.b
        packets = packets(head, 2)
        ident, comment = packets
        return nil unless ident&.start_with?("\x01vorbis".b) && ident.bytesize >= 16
        return nil unless comment&.start_with?("\x03vorbis".b)

        rate = ident.byteslice(12, 4).unpack1("V")
        comments = AudioTags.vorbis_comments(comment, 7)
        picture = picture_from(comments["METADATA_BLOCK_PICTURE"])
        AudioTags.tags_from_comments(comments, duration(file, rate), picture)
      end

      def self.picture_from(encoded)
        return nil if encoded.blank?

        Flac.picture_data(Base64.decode64(encoded).b)
      rescue StandardError
        nil
      end

      # The first `count` packets, put back together from page segments.
      def self.packets(data, count)
        packets = []
        current = "".b
        offset = 0
        while packets.size < count && offset + 27 <= data.bytesize
          return packets unless data.byteslice(offset, 4) == "OggS"

          segments = data.getbyte(offset + 26)
          table = data.byteslice(offset + 27, segments).to_s.bytes
          return packets if table.size < segments

          cursor = offset + 27 + segments
          table.each do |length|
            return packets if cursor + length > data.bytesize

            current << data.byteslice(cursor, length)
            cursor += length
            next if length == 255

            packets << current
            current = "".b
          end
          offset = cursor
        end
        packets
      end

      # The granule position of the last page is the total sample count.
      def self.duration(file, rate)
        return nil unless rate.positive?

        tail_size = [ file.size, 64.kilobytes ].min
        file.seek(file.size - tail_size)
        tail = file.read(tail_size).to_s.b
        at = tail.rindex("OggS".b)
        return nil if at.nil? || tail.bytesize < at + 14

        granule = tail.byteslice(at + 6, 8).unpack1("q<")
        (granule * 1000.0 / rate).round if granule.positive?
      end
    end

    module Mp4
      def self.read(file)
        moov = nil
        offset = 0
        while offset + 8 <= file.size
          file.seek(offset)
          size, type = file.read(8).unpack("Na4")
          header = 8
          if size == 1
            size = file.read(8).to_s.unpack1("Q>")
            header = 16
          elsif size.zero?
            size = file.size - offset
          end
          return nil if size.nil? || size < header

          if type == "moov"
            return nil if size > MAX_TAG_BYTES

            moov = file.read(size - header)
            break
          end
          offset += size
        end
        return nil if moov.nil?

        parse(moov.b)
      end

      def self.parse(moov)
        tags = Tags.new
        child(moov, "mvhd") { |body| tags.duration_ms = duration(body) }
        child(moov, "udta") do |udta|
          child(udta, "meta") do |meta|
            child(meta.byteslice(4..).to_s, "ilst") { |ilst| items(tags, ilst) }
          end
        end
        tags
      end

      # Yields the body of the first direct child box named `name`.
      def self.child(data, name)
        each_box(data) do |type, body|
          next unless type == name

          yield body
          break
        end
      end

      def self.each_box(data)
        offset = 0
        while offset + 8 <= data.bytesize
          size, type = data.byteslice(offset, 8).unpack("Na4")
          size = data.bytesize - offset if size.zero?
          break if size < 8 || offset + size > data.bytesize

          yield type.b.force_encoding(Encoding::BINARY), data.byteslice(offset + 8, size - 8)
          offset += size
        end
      end

      def self.duration(body)
        if body.getbyte(0) == 1
          scale = body.byteslice(20, 4).unpack1("N")
          length = body.byteslice(24, 8).unpack1("Q>")
        else
          scale, length = body.byteslice(12, 8).unpack("N2")
        end
        (length * 1000.0 / scale).round if scale.to_i.positive? && length.to_i.positive?
      end

      def self.items(tags, ilst)
        each_box(ilst) do |type, body|
          each_box(body) do |inner, data|
            next unless inner == "data" && data.bytesize >= 8

            payload = data.byteslice(8..)
            case type.b
            when "\xA9nam".b then tags.title ||= AudioTags.text(payload)
            when "\xA9ART".b then tags.artist ||= AudioTags.text(payload)
            when "\xA9alb".b then tags.album ||= AudioTags.text(payload)
            when "trkn".b then tags.track ||= (payload.byteslice(2, 2).to_s.unpack1("n") || 0).then { |n| n if n.positive? }
            when "covr".b then tags.picture ||= payload
            end
          end
        end
      end
    end
  end
end
