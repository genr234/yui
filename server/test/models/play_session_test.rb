require "test_helper"

class PlaySessionTest < ActiveSupport::TestCase
  include PlaySessionTestHelper
  include ActionCable::TestHelper

  test "starts a session with a 4-letter code from the alphabet and a 32-byte qr key" do
    play_session, created = PlaySession.find_or_start_for!(create_kiosk)

    assert created
    assert_match(/\A[BCDFGHJKLMNPQRSTVWXZ]{4}\z/, play_session.code)
    assert_equal 32, Base64.strict_decode64(play_session.qr_key).bytesize
    assert_not play_session.lobby_open
  end

  test "returns the kiosk's active session instead of starting another" do
    kiosk = create_kiosk
    first, = PlaySession.find_or_start_for!(kiosk)
    second, created = PlaySession.find_or_start_for!(kiosk)

    assert_not created
    assert_equal first, second

    first.end!
    third, created = PlaySession.find_or_start_for!(kiosk)
    assert created
    assert_not_equal first, third
  end

  test "codes are unique among active sessions and retried on collision" do
    taken = start_session
    codes = [ taken.code, taken.code, "ZZZZ" ]
    original = PlaySession.method(:generate_code)
    PlaySession.define_singleton_method(:generate_code) { codes.shift }
    begin
      play_session, = PlaySession.find_or_start_for!(create_kiosk)
      assert_equal "ZZZZ", play_session.code
    ensure
      PlaySession.define_singleton_method(:generate_code, original)
    end

    duplicate = PlaySession.new(kiosk: create_kiosk, code: taken.code)
    assert_not duplicate.valid?
    assert duplicate.errors.of_kind?(:code, :taken)
  end

  test "ending a session clears every player's avatar" do
    play_session = start_session
    selfie, = join_player(play_session, name: "Selfie", avatar: jpeg_avatar)
    emoji, = join_player(play_session, name: "Emoji", avatar: "\u{1F98A}")
    kicked, = join_player(play_session, name: "Kicked", avatar: jpeg_avatar)
    play_session.kick!(kicked)

    play_session.end!

    assert_equal [ nil, nil, nil ], [ selfie, emoji, kicked ].map { |player| player.reload.avatar }
  end

  test "an ended session's code can be reused" do
    ended = start_session
    ended.end!

    reused = PlaySession.new(kiosk: create_kiosk, code: ended.code)
    assert reused.valid?
  end

  test "accepts qr tokens from the current and previous 30 s windows only" do
    play_session = start_session
    now = Time.utc(2026, 11, 8, 12, 0, 15)

    current = play_session.qr_token_for(now)
    previous = play_session.qr_token_for(now - 30)
    expired = play_session.qr_token_for(now - 60)

    assert_match(/\A[A-Z2-7]{8}\z/, current)
    assert play_session.valid_qr_token?(current, now)
    assert play_session.valid_qr_token?(current.downcase, now)
    assert play_session.valid_qr_token?(previous, now)
    assert_not play_session.valid_qr_token?(expired, now)
    assert_not play_session.valid_qr_token?("AAAAAAAA", now)
    assert_not play_session.valid_qr_token?(nil, now)
    assert_not start_session.valid_qr_token?(current, now)
  end

  test "qr token matches base32(HMAC-SHA256(key, big-endian counter))[0, 8]" do
    play_session = start_session
    time = Time.at(1_767_225_600)
    digest = OpenSSL::HMAC.digest("SHA256", Base64.strict_decode64(play_session.qr_key), [ 1_767_225_600 / 30 ].pack("Q>"))

    assert_equal PlaySession.base32(digest)[0, 8], play_session.qr_token_for(time)
    # RFC 4648 test vector
    assert_equal "MZXW6YTBOI", PlaySession.base32("foobar")
  end

  test "ending notifies every active player" do
    play_session = start_session
    player, = join_player(play_session)

    assert_broadcast_on(PlaySessionChannel.broadcasting_for(player), { type: "session_ended" }) do
      play_session.end!
    end
    assert_not play_session.reload.active?
  end

  test "players silent past the presence timeout lose slot, host and speaker" do
    play_session = start_session
    ghost, = join_player(play_session, name: "Ghost")
    ghost.mark_connected!("g")
    guest, = join_player(play_session, name: "Guest", color: "red")
    guest.mark_connected!("h")

    travel Player::PRESENCE_TIMEOUT - 10.seconds do
      guest.touch_presence!("h")
      assert_empty play_session.release_stale_players!
      assert ghost.reload.connected
    end

    travel Player::PRESENCE_TIMEOUT + 10.seconds do
      guest.touch_presence!("h")
      assert_equal [ ghost ], play_session.release_stale_players!
      ghost.reload
      assert_not ghost.connected
      assert_nil ghost.slot
      assert_not ghost.host
      assert_not ghost.speaker
      assert guest.reload.host
      assert guest.speaker
      assert_equal 2, guest.slot

      newcomer, = join_player(play_session, name: "New", color: "green")
      assert_equal 1, newcomer.slot
    end
  end

  test "releasing a connected player tells the kiosk it left" do
    play_session = start_session
    ghost, = join_player(play_session)
    ghost.mark_connected!("g")

    travel Player::PRESENCE_TIMEOUT + 1.minute do
      assert_broadcasts(PlaySessionChannel.broadcasting_for(play_session), 1) do
        play_session.release_stale_players!
      end
    end
  end

  test "a released phone that comes back takes a free slot again" do
    play_session = start_session
    ghost, = join_player(play_session)
    travel Player::PRESENCE_TIMEOUT + 1.minute do
      play_session.release_stale_players!
      assert_nil ghost.reload.slot
      assert ghost.reclaim_roles!
      assert_equal 1, ghost.slot
      assert ghost.host
      assert ghost.speaker
    end
  end

  test "a connected spectator takes the seat of a released player" do
    play_session = start_session
    players = Array.new(5) { |i| join_player(play_session, name: "P#{i}", color: Player::COLORS[i]).first }
    spectator = players.last
    assert_nil spectator.slot
    spectator.mark_connected!("s")
    travel Player::PRESENCE_TIMEOUT + 1.minute do
      spectator.touch_presence!("s")
      play_session.release_stale_players!
      assert_equal 1, spectator.reload.slot
      assert spectator.host
    end
  end

  test "reuse drops players that are not present anymore" do
    kiosk = create_kiosk
    play_session = start_session(kiosk)
    ghost, = join_player(play_session)
    ghost.mark_connected!("g")

    travel Player::PRESENCE_TIMEOUT + 1.minute do
      reused, created = PlaySession.find_or_start_for!(kiosk)
      assert_not created
      assert_equal play_session, reused
      assert_not ghost.reload.connected
      assert_nil ghost.slot
    end
  end

  test "an idle session is not reused, a session with a connected player is" do
    kiosk = create_kiosk
    play_session = start_session(kiosk)
    player, = join_player(play_session)
    player.mark_connected!("n")

    travel PlaySession::REUSE_IDLE_LIMIT + 5.minutes do
      player.touch_presence!("n")
      reused, created = PlaySession.find_or_start_for!(kiosk)
      assert_not created
      assert_equal play_session, reused
    end

    player.mark_disconnected!("n")
    travel PlaySession::REUSE_IDLE_LIMIT * 2 + 5.minutes do
      fresh, created = PlaySession.find_or_start_for!(kiosk)
      assert created
      assert_not_equal play_session, fresh
      assert_not play_session.reload.active?
    end
  end

  def seated_pair
    play_session = start_session
    giver, = join_player(play_session, name: "Ana")
    4.times { |n| join_player(play_session, name: "Fill#{n}") } # fills 2-4, one spectator left over
    spectator = play_session.players.active.find_by(slot: nil)
    spectator.mark_connected!("s")
    [ play_session, giver.reload, spectator.reload ]
  end

  test "pass_seat! swaps the slot and keeps host and speaker with the person" do
    play_session, giver, spectator = seated_pair
    assert giver.host?
    assert giver.speaker?

    assert_broadcasts(PlaySessionChannel.broadcasting_for(play_session), 2) do
      play_session.pass_seat!(giver, spectator, slot: 1)
    end

    assert_equal 1, spectator.reload.slot
    assert_nil giver.reload.slot
    assert giver.host?
    assert giver.speaker?
    assert_not spectator.host?
    assert_equal [ 1, 2, 3, 4 ], play_session.players.active.where.not(slot: nil).order(:slot).pluck(:slot)
  end

  test "pass_seat! refuses a recipient who is seated, disconnected, kicked or the giver" do
    play_session, giver, spectator = seated_pair
    seated = play_session.players.active.find_by(slot: 2)
    seated.mark_connected!("x")

    error = assert_raises(PlaySession::SeatPassError) { play_session.pass_seat!(giver, seated) }
    assert_equal :recipient_not_spectator, error.code
    assert_equal 1, giver.reload.slot, "a refused pass changes nothing"
    assert_equal 2, seated.reload.slot

    spectator.update!(connected: false)
    error = assert_raises(PlaySession::SeatPassError) { play_session.pass_seat!(giver, spectator) }
    assert_equal :recipient_not_spectator, error.code
    assert_equal 1, giver.reload.slot

    spectator.update!(connected: true)
    play_session.kick!(spectator)
    assert_raises(PlaySession::SeatPassError) { play_session.pass_seat!(giver, spectator) }
    assert_equal 1, giver.reload.slot

    assert_equal :same_player, assert_raises(PlaySession::SeatPassError) { play_session.pass_seat!(giver, giver) }.code
  end

  test "pass_seat! refuses when the giver lost the seat to a new joiner" do
    play_session = start_session
    giver, = join_player(play_session, name: "Ana")
    3.times { |n| join_player(play_session, name: "Fill#{n}") }
    spectator, = join_player(play_session, name: "Dee")
    late, = join_player(play_session, name: "Late")
    [ spectator, late ].each { |player| player.mark_connected!("c") }

    # Ana goes stale; the longest-waiting connected spectator (Dee) gets slot 1.
    giver.update_columns(last_seen_at: 10.minutes.ago)
    play_session.release_stale_players!
    assert_nil giver.reload.slot
    assert_equal 1, spectator.reload.slot

    error = assert_raises(PlaySession::SeatPassError) { play_session.pass_seat!(giver, late, slot: 1) }
    assert_equal :giver_not_seated, error.code
    assert_equal 1, spectator.reload.slot

    # Slot taken by a different holder than the offer was made for.
    other = play_session.players.active.find_by(slot: 2)
    error = assert_raises(PlaySession::SeatPassError) { play_session.pass_seat!(other, late, slot: 1) }
    assert_equal :seat_changed, error.code
    assert_equal 2, other.reload.slot
    assert_nil late.reload.slot
  end

  test "a unique-index collision rolls the whole pass back" do
    play_session, giver, spectator = seated_pair
    # Simulate a joiner grabbing slot 1 between the two updates.
    Player.connection.execute("CREATE TRIGGER grab_seat AFTER UPDATE OF slot ON players WHEN NEW.slot IS NULL AND OLD.slot = 1 " \
      "BEGIN UPDATE players SET slot = 1 WHERE id = #{play_session.players.active.find_by(slot: 4).id}; END")
    error = assert_raises(PlaySession::SeatPassError) { play_session.pass_seat!(giver, spectator, slot: 1) }
    assert_equal :seat_changed, error.code
    assert_equal 1, giver.reload.slot
    assert_nil spectator.reload.slot
  ensure
    Player.connection.execute("DROP TRIGGER IF EXISTS grab_seat")
  end
end
