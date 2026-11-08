class Kiosk < ApplicationRecord
  belongs_to :account
  has_many :kiosk_commands, dependent: :destroy
  has_many :kiosk_operations, dependent: :nullify

  # Yui Pad play sessions
  has_many :play_sessions, dependent: :destroy
  # Asset library sync state (Phase 4).
  has_many :kiosk_assets, dependent: :delete_all

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
end
