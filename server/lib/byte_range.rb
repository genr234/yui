# Parses a single-range HTTP Range header ("bytes=N-", "bytes=N-M", "bytes=-N").
#
#   ByteRange.parse(header, total) # => nil (no/ignored Range: send everything),
#                                  #    :unsatisfiable (416), or an inclusive Range
#
# Multi-range requests and malformed headers are ignored (RFC 9110 allows serving the
# whole representation instead).
module ByteRange
  PATTERN = /\Abytes=(\d*)-(\d*)\z/

  def self.parse(header, total)
    match = PATTERN.match(header.to_s.strip.delete(" "))
    return if match.nil?

    first, last = match[1], match[2]
    return if first.empty? && last.empty?

    if first.empty?
      # Suffix range: the last N bytes.
      length = last.to_i
      return :unsatisfiable if length.zero? || total.zero?

      start = [ total - length, 0 ].max
      return start..(total - 1)
    end

    start = first.to_i
    return :unsatisfiable if start >= total

    finish = last.empty? ? total - 1 : [ last.to_i, total - 1 ].min
    return if finish < start

    start..finish
  end
end
