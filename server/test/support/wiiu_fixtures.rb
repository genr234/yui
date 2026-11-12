# Builds tiny synthetic Wii U files (headers and a minimal ZArchive footer, never game data) for
# the asset analyzer tests. Layout per Exzap/ZArchive; see AssetAnalyzers::WiiUTitle.
module WiiuFixtures
  ZARCHIVE_MAGIC = 0x169F52D6
  ZARCHIVE_VERSION_1 = 0x61BF3A01

  # A name table and a file tree for the given root children, each [name, :dir | :file].
  # Directories get no children of their own (the analyzer only reads the root).
  def zarchive_tree(children)
    names = "".b
    offsets = children.map do |name, _kind|
      offset = names.bytesize
      bytes = name.b
      names << (bytes.bytesize >= 0x80 ? [ (bytes.bytesize & 0x7F) | 0x80, bytes.bytesize >> 7 ].pack("CC") : [ bytes.bytesize ].pack("C")) << bytes
      offset
    end
    root_name = names.bytesize
    names << [ 0 ].pack("C") # the nameless root
    tree = [ root_name, 1, children.size, 0 ].pack("L>4")
    children.each_with_index do |(_name, kind), index|
      flag = kind == :file ? 0x80000000 : 0
      tree << [ flag | offsets[index], 0, 0, 0 ].pack("L>4")
    end
    [ names, tree ]
  end

  # A structurally valid ZArchive: an empty compressed-data section, one offset record, the name
  # table, the file tree and the 144-byte footer.
  def wua_image(children: [ [ "0005000010101234_v16", :dir ] ], total_size: nil, magic: ZARCHIVE_MAGIC, version: ZARCHIVE_VERSION_1)
    names, tree = zarchive_tree(children)
    offset_records = ("\0" * 40).b
    body = "".b
    positions = {}
    { data: "".b, records: offset_records, names: names, tree: tree, meta_dir: "".b, meta_data: "".b }.each do |key, bytes|
      positions[key] = [ body.bytesize, bytes.bytesize ]
      body << bytes
    end
    footer = positions.values.flatten.pack("Q>12") + ("\0" * 32).b
    size = body.bytesize + 144
    footer << [ total_size || size, version, magic ].pack("Q>L>L>")
    body + footer
  end

  def wux_image
    "WUX0".b + [ 0x2E023FD0 ].pack("L<") + ("\0" * 64).b
  end

  def wud_image(product_code: "WUP-P-ALCP")
    product_code.b + ("\0" * 128).b
  end
end
