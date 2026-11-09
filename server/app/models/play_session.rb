class PlaySession < ApplicationRecord
  CODE_ALPHABET = "BCDFGHJKLMNPQRSTVWXZ".freeze
  CODE_LENGTH = 4
  CODE_ATTEMPTS = 20
  QR_WINDOW = 30 # seconds
  QR_TOKEN_LENGTH = 8
  BASE32_ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567".freeze
  # A session ends on its own once its kiosk (Kiosk#last_seen_at) has been silent this long.
  KIOSK_TIMEOUT = 10.minutes

  belongs_to :kiosk
  has_many :players, dependent: :destroy

  scope :active, -> { where(ended_at: nil) }
  # Active sessions started before the cutoff whose kiosk hasn't been seen since.
  scope :abandoned, ->(now = Time.current) {
    cutoff = now - KIOSK_TIMEOUT
    active.joins(:kiosk).where(created_at: ...cutoff)
      .where("kiosks.last_seen_at IS NULL OR kiosks.last_seen_at < ?", cutoff)
  }

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
    # Avatars (selfies especially) live only as long as the session.
    players.where.not(avatar: nil).update_all(avatar: nil, updated_at: Time.current)
    players.active.find_each do |player|
      PlaySessionChannel.broadcast_to(player, { type: "session_ended" })
      player.disconnect_later
    end
  end

  def kick!(player)
    raise ArgumentError, "player is not in this session" unless player.play_session_id == id
    return if player.kicked?

    seated = nil
    transaction do
      was_host = player.host?
      was_speaker = player.speaker?
      freed_slot = player.slot
      player.update!(kicked_at: Time.current, host: false, speaker: false, connected: false, connection_nonce: nil)
      seated = seat_spectator!(freed_slot) if freed_slot
      assign_new_host! if was_host
      give_speaker_to_host! if was_speaker
    end
    seated&.broadcast_updated
    PlaySessionChannel.broadcast_to(player, { type: "kicked" })
    player.disconnect_later
  end

  # A freed seat goes to the spectator who has waited longest, preferring one
  # whose phone is connected. Returns that player (not yet announced) or nil.
  def seat_spectator!(slot)
    spectators = players.active.where(slot: nil).order(:id)
    spectator = spectators.find_by(connected: true) || spectators.first
    return unless spectator

    spectator.update!(slot: slot)
    spectator
  end

  # Moves the speaker role (the one phone that plays the kiosk's sound).
  # Exactly one active player holds it; the kiosk reroutes the audio track.
  def transfer_speaker!(player)
    raise ArgumentError, "player is not in this session" unless player.play_session_id == id
    raise ArgumentError, "player was kicked" if player.kicked?
    return if player.speaker?

    previous = nil
    transaction do
      previous = players.active.find_by(speaker: true)
      previous&.update!(speaker: false)
      player.update!(speaker: true)
    end
    previous&.broadcast_updated
    player.broadcast_updated
  end

  # The speaker's phone went away (cable unsubscribed): the host becomes the
  # speaker. A host who was the speaker keeps the role until it comes back.
  def speaker_left!(player)
    return unless player.speaker? && !player.host?

    host = players.active.where.not(id: player.id).find_by(host: true)
    transfer_speaker!(host) if host
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

  # The requested color, or the first free one when another active player
  # already has it. Two phones that open the join screen together both see
  # the same free color, so the join itself settles it.
  def free_color(preferred, except: nil)
    taken = players.active.where.not(id: except&.id).pluck(:color)
    return preferred unless taken.include?(preferred)

    Player::COLORS.find { |color| !taken.include?(color) } || preferred
  end

  # After the speaker was kicked: the (possibly new) host takes the role.
  def give_speaker_to_host!
    host = players.active.find_by(host: true)
    return unless host && !players.active.exists?(speaker: true)

    host.update!(speaker: true)
    host.broadcast_updated
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
