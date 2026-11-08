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
end
