module Api
  module Kiosk
    # Controller health from kiosks: recent events plus a current snapshot
    # (Chrome state, version, uptime), shown on the kiosk's control room page.
    class TelemetryController < BaseController
      MAX_EVENTS = 300

      def create
        touch_current_kiosk!
        events = Array(params[:events]).first(MAX_EVENTS)
        rows = events.filter_map { |event| row_for(event) }
        KioskEvent.insert_all(rows) if rows.any?
        KioskEvent.prune!(current_kiosk)

        snapshot = params[:snapshot]
        if snapshot.respond_to?(:to_unsafe_h)
          current_kiosk.update!(health: snapshot.to_unsafe_h.slice(*SNAPSHOT_KEYS), health_at: Time.current)
        end
        head :no_content
      end

      private

      SNAPSHOT_KEYS = %w[
        version commit controller_started_at uptime_seconds chrome chrome_since chrome_restarts console_lines_dropped
      ].freeze

      def row_for(event)
        return unless event.respond_to?(:to_unsafe_h)

        hash = event.to_unsafe_h
        kind = hash["kind"].to_s.first(64)
        at = Time.zone.parse(hash["at"].to_s) rescue nil
        return if kind.blank? || at.nil?

        data = hash["data"].is_a?(Hash) ? hash["data"] : {}
        data = data.transform_values { |value| value.is_a?(String) ? value.first(500) : value }
        now = Time.current
        { kiosk_id: current_kiosk.id, kind: kind, occurred_at: [ at, now ].min, data: data, created_at: now, updated_at: now }
      end
    end
  end
end
