# Asset analyzers fill `metadata` for uploaded assets. They are the only place that
# knows about specific file formats or consoles; the rest of the platform only sees
# generic kinds (`game`, `runtime`, `media`, `other`) and the metadata they produce.
#
# An analyzer responds to `call(path, base_url)` and returns an AssetAnalyzers::Result,
# or nil when it doesn't recognise the file. The first match wins.
module AssetAnalyzers
  Result = Data.define(:title, :metadata)

  # Every game gets these keys, even when no analyzer recognised it.
  GAME_DEFAULTS = { "platform" => nil, "format" => "unknown" }.freeze

  def self.for_games
    [ NintendoDisc, WiiUTitle, RomCartridge ]
  end

  def self.for_media
    [ AudioTags ]
  end

  def self.analyze_game(path, base_url)
    first_result(for_games, path, base_url)
  end

  def self.analyze_media(path, base_url)
    first_result(for_media, path, base_url)
  end

  def self.first_result(analyzers, path, base_url)
    analyzers.each do |analyzer|
      result = analyzer.call(path, base_url)
      return result if result
    end
    nil
  end
end
