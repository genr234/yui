class Kiosk < ApplicationRecord
  belongs_to :account
  has_many :kiosk_commands, dependent: :destroy
  has_many :kiosk_operations, dependent: :nullify

  # Yui Pad play sessions
  has_many :play_sessions, dependent: :destroy
  # Asset library sync state (Phase 4).
  has_many :kiosk_assets, dependent: :delete_all
  # Controller health reported with POST /api/kiosk/telemetry.
  has_many :kiosk_events, dependent: :delete_all

  validates :name, :device_uid, :device_token_digest, presence: true
  validates :device_uid, uniqueness: { scope: :account_id }
  validates :device_token_digest, uniqueness: true

  def self.digest_token(token)
    Digest::SHA256.hexdigest(token.to_s)
  end

  def self.issue_token
    SecureRandom.hex(32)
  end

  def self.authenticate(token)
    find_by(device_token_digest: digest_token(token))
  end

  def touch_seen!
    update!(last_seen_at: Time.current)
  end

  ONLINE_WINDOW = 10.minutes

  def online?
    last_seen_at.present? && last_seen_at > ONLINE_WINDOW.ago
  end

  # "running", "restarting", "start_failed", "starting", "stopped" or nil when
  # the kiosk has never reported (older controller).
  def chrome_state
    health.to_h["chrome"].presence
  end

  CHROME_LABELS = {
    "running" => "Chrome running", "starting" => "Chrome starting", "restarting" => "Chrome restarting",
    "start_failed" => "Chrome won't start", "stopped" => "Yui stopped"
  }.freeze

  def chrome_label
    return "No health reports yet" if chrome_state.nil?
    return "Offline (last: #{CHROME_LABELS[chrome_state] || chrome_state})" unless online?

    CHROME_LABELS[chrome_state] || chrome_state
  end

  # Fine when Chrome is running and the kiosk is online.
  def healthy?
    online? && chrome_state == "running"
  end
end
