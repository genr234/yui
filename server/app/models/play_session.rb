class PlaySession < ApplicationRecord
  CODE_ALPHABET = "BCDFGHJKLMNPQRSTVWXZ".freeze
  CODE_LENGTH = 4
  CODE_ATTEMPTS = 20
  QR_WINDOW = 30 # seconds
  QR_TOKEN_LENGTH = 8
  BASE32_ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567".freeze

  belongs_to :kiosk
  has_many :players, dependent: :destroy

  scope :active, -> { where(ended_at: nil) }

  before_validation :assign_qr_key, on: :create

  validates :code, presence: true, length: { is: CODE_LENGTH },
    format: { with: /\A[#{CODE_ALPHABET}]+\z/ }
  validates :code, uniqueness: { conditions: -> { active } }, if: :active?
  validates :qr_key, presence: true
  validates :kiosk_id, uniqueness: { conditions: -> { active }, message: "already has an active session" }, if: :active?

  def self.generate_code
    Array.new(CODE_LENGTH) { CODE_ALPHABET[SecureRandom.random_number(CODE_ALPHABET.length)] }.join
  end

  def self.normalize_code(code)
    code.to_s.strip.upcase
  end

  def self.find_active_by_code(code)
    active.find_by(code: normalize_code(code))
  end

  # Returns the kiosk's active session, or starts one with a fresh code that is
  # unique among active sessions (retried on collision).
  def self.find_or_start_for!(kiosk)
    existing = kiosk.play_sessions.active.first
    return [ existing, false ] if existing

    CODE_ATTEMPTS.times do
      session = kiosk.play_sessions.new(code: generate_code)
      next if session.code_taken?

      begin
        session.save!
        return [ session, true ]
      rescue ActiveRecord::RecordNotUnique, ActiveRecord::RecordInvalid => error
        # Lost a race: either another request started this kiosk's session, or
        # the code was taken in the meantime (then try another code).
        existing = kiosk.play_sessions.active.first
        return [ existing, false ] if existing
        raise if error.is_a?(ActiveRecord::RecordInvalid) && !session.errors.of_kind?(:code, :taken)
      end
    end

    raise ActiveRecord::RecordNotSaved.new("Could not allocate a unique play session code", nil)
  end

  def code_taken?
    PlaySession.active.where(code: code).where.not(id: id).exists?
  end

  def active?
    ended_at.nil?
  end

  # 32 random bytes; the key used for the HMAC is the decoded bytes.
  def qr_key_bytes
    Base64.strict_decode64(qr_key)
  end

  # qr_token = base32(HMAC-SHA256(qr_key, counter))[0, 8], counter = floor(unix / 30)
  # encoded as an 8-byte big-endian integer (the RFC 6238 TOTP convention).
  def qr_token_for(time)
    counter = time.to_i / QR_WINDOW
    digest = OpenSSL::HMAC.digest("SHA256", qr_key_bytes, [ counter ].pack("Q>"))
    self.class.base32(digest)[0, QR_TOKEN_LENGTH]
  end

  def valid_qr_token?(token, now = Time.current)
    candidate = token.to_s.strip.upcase
    return false unless candidate.length == QR_TOKEN_LENGTH

    [ now, now - QR_WINDOW ].map { |time| ActiveSupport::SecurityUtils.secure_compare(qr_token_for(time), candidate) }.any?
  end

  # RFC 4648 base32, uppercase, no padding.
  def self.base32(bytes)
    bits = bytes.unpack1("B*")
    bits.scan(/.{1,5}/).map { |chunk| BASE32_ALPHABET[chunk.ljust(5, "0").to_i(2)] }.join
  end

  def active_players
    players.active
  end

  def end!
    return unless active?

    update!(ended_at: Time.current)
    players.active.find_each do |player|
      PlaySessionChannel.broadcast_to(player, { type: "session_ended" })
    end
  end

  def kick!(player)
    raise ArgumentError, "player is not in this session" unless player.play_session_id == id
    return if player.kicked?

    transaction do
      was_host = player.host?
      player.update!(kicked_at: Time.current, host: false, connected: false, connection_nonce: nil)
      assign_new_host! if was_host
    end
    PlaySessionChannel.broadcast_to(player, { type: "kicked" })
  end

  def transfer_host!(player)
    raise ArgumentError, "player is not in this session" unless player.play_session_id == id
    raise ArgumentError, "player was kicked" if player.kicked?
    return if player.host?

    previous = nil
    transaction do
      previous = players.active.find_by(host: true)
      previous&.update!(host: false)
      player.update!(host: true)
    end
    previous&.broadcast_updated
    player.broadcast_updated
  end

  # When the host leaves for good, the lowest connected slot becomes host
  # (falling back to any slotted player, then the earliest spectator).
  def assign_new_host!
    candidates = players.active.order(Arel.sql("slot IS NULL"), :slot, :id)
    new_host = candidates.find_by(connected: true) || candidates.first
    return unless new_host

    new_host.update!(host: true)
    new_host.broadcast_updated
  end

  def free_slot
    taken = players.active.where.not(slot: nil).pluck(:slot)
    (Player::SLOTS.to_a - taken).min
  end

  def as_kiosk_json(base_url:)
    {
      id: id.to_s,
      code: code,
      qr_key: qr_key,
      join_base_url: "#{base_url}/play/#{code}",
      lobby_open: lobby_open,
      players: players.active.order(:id).map(&:as_player_json)
    }
  end

  private

  def assign_qr_key
    self.qr_key ||= Base64.strict_encode64(SecureRandom.random_bytes(32))
  end
end
