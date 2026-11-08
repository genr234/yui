# A Rack body that yields `length` bytes of a file starting at `offset`, in chunks,
# so multi-GB downloads never sit in memory. It intentionally has no #to_path.
class FileRangeBody
  CHUNK = 1.megabyte

  def initialize(path, offset, length)
    @path = path
    @offset = offset
    @length = length
  end

  def each
    remaining = @length
    File.open(@path, "rb") do |file|
      file.seek(@offset)
      while remaining.positive? && (chunk = file.read([ CHUNK, remaining ].min))
        remaining -= chunk.bytesize
        yield chunk
      end
    end
  end
end
