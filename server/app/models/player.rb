class Player < ApplicationRecord
  SLOTS = (1..4)
  COLORS = %w[blue red green yellow purple orange pink teal].freeze
  NAME_MAX = 20
  # Stereo speaker pair (contracts §2); nil is the mono speaker.
  SPEAKER_CHANNELS = %w[left right].freeze
  SLOT_ATTEMPTS = 5
  # A player silent for this long (no cable presence) is disconnected and
  # loses its slot and roles. The cable touches last_seen_at every
  # PRESENCE_TOUCH while subscribed.
  PRESENCE_TIMEOUT = 90.seconds
  PRESENCE_TOUCH = 30.seconds
  # Avatars (contracts §2): a creature built from parts (optionally with a
  # selfie that fills its body), or from older pads one emoji or a small round
  # selfie. Kept only while the session runs (PlaySession#end! clears them).
  AVATAR_EMOJI_MAX_BYTES = 16
  AVATAR_IMAGE_MAX_BYTES = 12.kilobytes
  AVATAR_IMAGE_PATTERN = %r{\Adata:image/(jpeg|webp);base64,([A-Za-z0-9+/]+={0,2})\z}
  AVATAR_EMOJI_PATTERN = /\p{Extended_Pictographic}|\p{Regional_Indicator}/
  # yui:c1:<8 part numbers>:<up to 3 eyes x-y-r>:<mouth x-y>:<nose x-y>[|<selfie>]
  # Mirrors avatar/src/format.ts.
  AVATAR_CREATURE_PREFIX = "yui:c1:"
  AVATAR_CREATURE_PATTERN = %r{
    \Ayui:c1:
    (?<parts>\d{1,3}(?:\.\d{1,3}){7}):
    (?<eyes>(?:\d{1,3}-\d{1,3}-\d{1,3}(?:,\d{1,3}-\d{1,3}-\d{1,3}){0,2})?):
    (?<mouth>\d{1,3}-\d{1,3}):
    (?<nose>\d{1,3}-\d{1,3})
    (?:\|(?<photo>data:[^|]+))?\z
  }x
  # How many choices each part has: body, eye style, mouth, nose, brow, top, arms, legs.
  AVATAR_CREATURE_PART_COUNTS = [ 6, 8, 15, 5, 4, 7, 6, 6 ].freeze
  AVATAR_EYE_RADIUS = (4..40)

  belongs_to :play_session
  # Set while the player plays as a Yui account (plan 10); never sent to the kiosk.
  belongs_to :account, optional: true
  belongs_to :phone_link, optional: true

  scope :active, -> { where(kicked_at: nil) }

  normalizes :name, with: ->(name) { name.to_s.strip }
  normalizes :avatar, with: ->(avatar) { avatar.to_s.strip.presence }

  validates :name, presence: true, length: { maximum: NAME_MAX }
  validates :color, inclusion: { in: COLORS }
  validates :speaker_channel, inclusion: { in: SPEAKER_CHANNELS }, allow_nil: true
  validates :slot, inclusion: { in: SLOTS }, allow_nil: true
  validates :token_digest, presence: true
  validate :avatar_is_known_kind

  def self.digest_token(token)
    Digest::SHA256.hexdigest(token.to_s)
  end

  def self.issue_token
    SecureRandom.urlsafe_base64(32)
  end

  # Joins a session: lowest free slot 1-4 among non-kicked players, otherwise a
  # spectator (slot nil). The first player becomes host, and the speaker (the
  # phone that plays the kiosk's sound) whenever nobody holds that role.
  # Returns [player, raw token].
  def self.join!(play_session, name:, color:, avatar: nil)
    token = issue_token
    play_session.release_stale_players!
    attempts = 0
    begin
      attempts += 1
      player = transaction do
        play_session.players.create!(
          name: name,
          avatar: avatar,
          color: play_session.free_color(color),
          slot: play_session.free_slot,
          host: !play_session.players.active.exists?(host: true),
          speaker: !play_session.players.active.exists?(speaker: true),
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

  # Plays as the link's account this session, or as a guest with nil.
  # Announces the change to the kiosk and the phone.
  def use_account!(link)
    account = link&.live? ? link.account : nil
    return if account_id == account&.id && phone_link_id == link&.id

    update!(account: account, phone_link: account ? link : nil)
    link&.touch(:last_used_at) if account
    broadcast_updated
  end

  # The account the kiosk may use profiles for: linked, link still live.
  def profile_account
    account if account && phone_link&.live?
  end

  # Usable for signaling: not kicked and the session is still running.
  def playable?
    !kicked? && play_session.active?
  end

  def mark_connected!(nonce)
    update!(connected: true, connection_nonce: nonce, last_seen_at: Time.current)
  end

  # Refreshes presence while this subscription is the live one.
  def touch_presence!(nonce)
    self.class.where(id: id, connection_nonce: nonce).update_all(last_seen_at: Time.current)
  end

  # A phone that comes back after losing its slot (see
  # PlaySession#release_stale_players!) takes a free one, and host/speaker
  # when nobody holds them. Returns true when something changed.
  def reclaim_roles!
    return false if slot || kicked?

    attrs = {}
    free = play_session.free_slot
    attrs[:slot] = free if free
    attrs[:host] = true if free && !play_session.players.active.exists?(host: true)
    attrs[:speaker] = true if free && !play_session.players.active.exists?(speaker: true)
    return false if attrs.empty?

    update!(attrs)
    true
  rescue ActiveRecord::RecordNotUnique
    reload
    false
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
      speaker: speaker,
      speaker_channel: speaker_channel,
      account_linked: account_id.present?,
      connected: connected,
      avatar: avatar
    }
  end

  # Tells the kiosk and the player itself about a change.
  def broadcast_updated
    message = { type: "player_updated", player: as_player_json }
    PlaySessionChannel.broadcast_to(play_session, message)
    PlaySessionChannel.broadcast_to(self, message)
  end

  private

  def avatar_is_known_kind
    return if avatar.nil?

    valid =
      if avatar.start_with?(AVATAR_CREATURE_PREFIX) then creature_avatar?(avatar)
      elsif avatar.start_with?("data:") then selfie_avatar?(avatar)
      else emoji_avatar?(avatar)
      end
    errors.add(:avatar, :invalid) unless valid
  end

  # A creature recipe with every number in range. Its optional photo is held
  # to the selfie rules, and the whole string to the same size cap.
  def creature_avatar?(value)
    return false if value.bytesize > AVATAR_IMAGE_MAX_BYTES

    match = AVATAR_CREATURE_PATTERN.match(value)
    return false unless match

    parts = match[:parts].split(".").map(&:to_i)
    return false unless parts.each_with_index.all? { |part, i| part < AVATAR_CREATURE_PART_COUNTS[i] }

    eyes = match[:eyes].split(",").map { |eye| eye.split("-").map(&:to_i) }
    return false unless eyes.all? { |x, y, r| x <= 100 && y <= 100 && AVATAR_EYE_RADIUS.cover?(r) }
    return false unless [ match[:mouth], match[:nose] ].all? { |at| at.split("-").map(&:to_i).all? { |n| n <= 100 } }

    match[:photo].nil? || selfie_avatar?(match[:photo])
  end

  # A single grapheme, no ASCII at all (so no letters, digits or markup), that
  # is or contains a pictograph or a flag.
  def emoji_avatar?(value)
    value.bytesize <= AVATAR_EMOJI_MAX_BYTES &&
      value.grapheme_clusters.length == 1 &&
      !value.match?(/[\x00-\x7F]/) &&
      value.match?(AVATAR_EMOJI_PATTERN)
  end

  # A base64 JPEG or WebP data URL under the size cap whose bytes really are
  # that format.
  def selfie_avatar?(value)
    return false if value.bytesize > AVATAR_IMAGE_MAX_BYTES

    match = AVATAR_IMAGE_PATTERN.match(value)
    return false unless match

    bytes = Base64.strict_decode64(match[2])
    case match[1]
    when "jpeg" then bytes.start_with?("\xFF\xD8\xFF".b)
    when "webp" then bytes.byteslice(0, 4) == "RIFF" && bytes.byteslice(8, 4) == "WEBP"
    end
  rescue ArgumentError
    false
  end
end
