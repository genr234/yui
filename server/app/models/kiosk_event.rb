# One health event a kiosk reported (Chrome started, exited, hung, an update
# began...). Kiosks upload them with POST /api/kiosk/telemetry; only the
# newest KEEP per kiosk are kept.
class KioskEvent < ApplicationRecord
  KEEP = 500

  belongs_to :kiosk

  validates :kind, presence: true, length: { maximum: 64 }
  validates :occurred_at, presence: true

  scope :recent, -> { order(occurred_at: :desc, id: :desc) }

  LABELS = {
    "controller_started" => "Yui started",
    "controller_stopped" => "Yui stopped",
    "starting_chrome" => "Starting Chrome",
    "chrome_started" => "Chrome started",
    "chrome_detached" => "Chrome handed off to another window",
    "chrome_process_ended" => "Chrome process ended",
    "chrome_exited" => "Chrome exited",
    "chrome_start_failed" => "Chrome failed to start",
    "chrome_hung" => "Chrome stopped responding; restarted",
    "chrome_restart_requested" => "Chrome restart requested",
    "update_started" => "Update started",
    "update_failed" => "Update check failed",
    "remote_input" => "Remote input"
  }.freeze

  def label
    LABELS[kind] || kind.humanize
  end

  def self.prune!(kiosk)
    cutoff = kiosk.kiosk_events.recent.offset(KEEP).pick(:occurred_at)
    kiosk.kiosk_events.where(occurred_at: ...cutoff).delete_all if cutoff
  end
end
