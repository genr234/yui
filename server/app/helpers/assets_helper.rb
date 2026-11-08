module AssetsHelper
  KIND_LABELS = {
    "game" => "Game",
    "runtime" => "Runtime",
    "media" => "Media",
    "other" => "Other"
  }.freeze

  # Reports newer than this count as "still moving" for the auto-refresh.
  LIVE_WINDOW = 2.minutes

  def asset_kind_label(kind)
    KIND_LABELS.fetch(kind, kind.to_s.humanize)
  end

  def asset_kind_options
    KIND_LABELS.map { |value, label| [ label, value ] }
  end

  # "2.1 / 3.4 GB" (both numbers in the larger number's unit).
  def asset_progress(done, total)
    units = %w[B KB MB GB TB]
    exponent = total.to_i.positive? ? [ (Math.log(total) / Math.log(1024)).floor, units.size - 1 ].min : 0
    scale = 1024.0**exponent
    format_number = ->(value) { exponent.zero? ? value.to_i.to_s : format("%.1f", value / scale) }
    "#{format_number.call(done.to_i)} / #{format_number.call(total.to_i)} #{units[exponent]}"
  end

  def asset_bytes_done(asset)
    asset.uploading? ? asset.stored_bytes : asset.byte_size
  end

  # One line per kiosk: "Front kiosk: 2.1 / 3.4 GB", "ready", "failed: …".
  def asset_kiosk_sync_label(asset, kiosk)
    record = asset.kiosk_assets.find { |item| item.kiosk_id == kiosk.id }
    status = case record&.state
    when nil then "waiting"
    when "downloading" then asset_progress(record.bytes_done, asset.byte_size)
    when "paused" then "paused at #{asset_progress(record.bytes_done, asset.byte_size)}"
    when "failed" then [ "failed", record.error.presence ].compact.join(": ")
    else record.state
    end
    "#{kiosk.name}: #{status}"
  end

  def asset_kiosk_sync_state(asset, kiosk)
    asset.kiosk_assets.find { |item| item.kiosk_id == kiosk.id }&.state || "waiting"
  end

  # True while something on the list is still changing (upload, processing, kiosk sync),
  # so the page keeps polling.
  def asset_library_active?(assets)
    assets.any? do |asset|
      case asset.status
      when "uploading" then asset.updated_at > LIVE_WINDOW.ago
      when "processing" then asset.updated_at > 1.hour.ago
      when "ready" then asset.kiosk_assets.any? { |item| item.active? && item.updated_at > LIVE_WINDOW.ago }
      else false
      end
    end
  end
end
