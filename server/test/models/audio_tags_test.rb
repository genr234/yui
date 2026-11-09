require "test_helper"

class AudioTagsTest < ActionDispatch::IntegrationTest
  include MediaFixtures

  setup { @dir = Pathname(Dir.mktmpdir("audio")) }
  teardown { FileUtils.rm_rf(@dir) }

  def analyze(name, bytes, base_url = "https://yui.example.com")
    path = @dir.join(name)
    File.binwrite(path, bytes)
    AssetAnalyzers::AudioTags.call(path.to_s, base_url)
  end

  def assert_tags(result, title:, artist:, album:, track:)
    assert_equal title, result.title
    assert_equal "audio", result.metadata["kind"]
    assert_equal [ title, artist, album, track ], result.metadata.values_at("title", "artist", "album", "track")
  end

  def assert_cover(result, bytes)
    key = result.metadata["cover_key"]
    assert_match(MediaCover::KEY_FORMAT, key)
    assert_equal "https://yui.example.com/media_covers/#{key}", result.metadata["cover_url"]
    assert_equal bytes, File.binread(MediaCover.path_for(key))
  end

  test "mp3 ID3v2.4 with art, Xing duration" do
    result = analyze("a.mp3", mp3_file(frames: 1000))
    assert_tags(result, title: "Tiny Song", artist: "The Fixtures", album: "Synthetic", track: 3)
    assert_equal (1000 * 1152 * 1000.0 / 44_100).round, result.metadata["duration_ms"]
    assert_cover(result, PNG)
  end

  test "mp3 ID3v2.3 with UTF-16 text, CBR duration estimate" do
    result = analyze("a.mp3", mp3_file(version: 3, encoding: 1))
    assert_tags(result, title: "Tiny Song", artist: "The Fixtures", album: "Synthetic", track: 3)
    assert_cover(result, PNG)
    assert_equal (417 * 8.0 / 128).round, result.metadata["duration_ms"]
  end

  test "mp3 without art or base url" do
    result = analyze("a.mp3", mp3_file(picture: nil), nil)
    assert_nil result.metadata["cover_key"]
    assert_nil result.metadata["cover_url"]
  end

  test "mp3 without tags still reports duration, and a bare file is nil" do
    assert_equal 26, analyze("a.mp3", mp3_audio).metadata["duration_ms"]
    assert_nil analyze("a.mp3", "no audio here".b)
  end

  test "flac: streaminfo duration, vorbis comments, picture" do
    result = analyze("a.flac", flac_file)
    assert_tags(result, title: "Flac Tune", artist: "The Fixtures", album: "Synthetic", track: 7)
    assert_equal 90_000, result.metadata["duration_ms"]
    assert_cover(result, PNG)
  end

  test "ogg vorbis: comments, duration from the last granule, embedded picture" do
    result = analyze("a.ogg", ogg_file(picture: PNG))
    assert_tags(result, title: "Ogg Tune", artist: "The Fixtures", album: "Synthetic", track: 2)
    assert_equal 12_000, result.metadata["duration_ms"]
    assert_cover(result, PNG)
  end

  test "m4a: ilst tags, covr, mvhd duration" do
    result = analyze("a.m4a", m4a_file)
    assert_tags(result, title: "Mp4 Tune", artist: "The Fixtures", album: "Synthetic", track: 5)
    assert_equal 200_000, result.metadata["duration_ms"]
    assert_cover(result, JPEG)
  end

  test "garbage, empty and truncated files give nil and never raise" do
    %w[mp3 flac ogg m4a mp4 oga].each do |extension|
      assert_nil analyze("a.#{extension}", "".b), extension
      assert_nil analyze("a.#{extension}", SecureRandom.random_bytes(500)), extension
    end
    [ mp3_file, flac_file, ogg_file(picture: PNG), m4a_file ].zip(%w[mp3 flac ogg m4a]).each do |bytes, extension|
      [ 3, 11, 40, bytes.bytesize / 2, bytes.bytesize - 1 ].each do |cut|
        assert_nothing_raised { analyze("t.#{extension}", bytes.byteslice(0, cut)) }
      end
    end
  end

  test "lying sizes don't raise" do
    id3 = "ID3\x04\x00\x00".b + syncsafe(0x0FFFFFFF)
    assert_nil analyze("a.mp3", id3 + "x".b * 20)
    huge_box = [ 0xFFFFFF00 ].pack("N") + "moov" + "x" * 20
    assert_nil analyze("a.m4a", huge_box)
  end

  test "other extensions are ignored" do
    assert_nil analyze("a.png", PNG)
    assert_nil analyze("a.txt", mp3_file)
  end

  test "the media registry and covers controller" do
    path = @dir.join("a.flac")
    File.binwrite(path, flac_file)
    result = AssetAnalyzers.analyze_media(path.to_s, "https://yui.example.com")
    key = result.metadata["cover_key"]

    get "/media_covers/#{key}"
    assert_response :success
    assert_equal "image/png", response.media_type
    assert_equal PNG, response.body.b
    get "/media_covers/#{"0" * 32}.png"
    assert_response :not_found
    get "/media_covers/..%2Fsecret.png"
    assert_response :not_found
  end
end
