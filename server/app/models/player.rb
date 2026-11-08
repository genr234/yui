class Player < ApplicationRecord
  SLOTS = (1..4)
  COLORS = %w[blue red green yellow purple orange pink teal].freeze
  NAME_MAX = 20
  SLOT_ATTEMPTS = 5

  belongs_to :play_session

  scope :active, -> { where(kicked_at: nil) }

  normalizes :name, with: ->(name) { name.to_s.strip }

  validates :name, presence: true, length: { maximum: NAME_MAX }
  validates :color, inclusion: { in: COLORS }
  validates :slot, inclusion: { in: SLOTS }, allow_nil: true
  validates :token_digest, presence: true

  def self.digest_token(token)
    Digest::SHA256.hexdigest(token.to_s)
  end

  def self.issue_token
    SecureRandom.urlsafe_base64(32)
  end

  # Joins a session: lowest free slot 1-4 among non-kicked players, otherwise a
  # spectator (slot nil). The first player becomes host. Returns [player, raw token].
  def self.join!(play_session, name:, color:)
    token = issue_token
    attempts = 0
    begin
      attempts += 1
      player = transaction do
        play_session.players.create!(
          name: name,
          color: play_session.free_color(color),
          slot: play_session.free_slot,
          host: !play_session.players.active.exists?(host: true),
          token_digest: digest_token(token)
        )
      end
    rescue ActiveRecord::RecordNotUnique
      retry if attempts < SLOT_ATTEMPTS
      raise
    end
    [ player, token ]
  end

  # Cookie value is "<player id>:<raw token>"; only the digest is stored.
  def self.cookie_value(player, token)
    "#{player.id}:#{token}"
  end

  def self.from_cookie(value)
    id, token = value.to_s.split(":", 2)
    return if id.blank? || token.blank?

    player = find_by(id: id)
    return unless player
    return unless ActiveSupport::SecurityUtils.secure_compare(player.token_digest, digest_token(token))

    player
  end

  def kicked?
    kicked_at.present?
  end

  # Usable for signaling: not kicked and the session is still running.
  def playable?
    !kicked? && play_session.active?
  end

  def mark_connected!(nonce)
    update!(connected: true, connection_nonce: nonce, last_seen_at: Time.current)
  end

  # Only the subscription that last connected can mark the player as gone, so a
  # quick reconnect (new socket before the old one closes) stays "connected".
  def mark_disconnected!(nonce)
    updated = self.class.where(id: id, connection_nonce: nonce)
      .update_all(connected: false, connection_nonce: nil, last_seen_at: Time.current, updated_at: Time.current)
    reload
    updated.positive?
  end

  # Closes this player's cable connection(s) after a kick or session end. A
  # reconnect is rejected, since the player is no longer playable.
  def disconnect_later
    DisconnectPlayerJob.set(wait: DisconnectPlayerJob::DELAY).perform_later(self)
  end

  def as_player_json
    {
      id: id.to_s,
      slot: slot,
      name: name,
      color: color,
      host: host,
      connected: connected
    }
  end

  # Tells the kiosk and the player itself about a change.
  def broadcast_updated
    message = { type: "player_updated", player: as_player_json }
    PlaySessionChannel.broadcast_to(play_session, message)
    PlaySessionChannel.broadcast_to(self, message)
  end
end
