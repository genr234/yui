require "test_helper"

class PlayerTest < ActiveSupport::TestCase
  include PlaySessionTestHelper
  include ActionCable::TestHelper

  test "assigns the lowest free slot, then spectators, and makes the first player host" do
    play_session = start_session
    players = 5.times.map { |index| join_player(play_session, name: "P#{index}").first }

    assert_equal [ 1, 2, 3, 4, nil ], players.map(&:slot)
    assert_equal [ true, false, false, false, false ], players.map(&:host)
  end

  test "a second player asking for a taken color gets the first free one" do
    play_session = start_session
    first, = Player.join!(play_session, name: "Ann", color: "blue")
    second, = Player.join!(play_session, name: "Bo", color: "blue")
    third, = Player.join!(play_session, name: "Cy", color: "green")

    assert_equal "blue", first.color
    assert_equal "red", second.color
    assert_equal "green", third.color

    first.update!(kicked_at: Time.current)
    fourth, = Player.join!(play_session, name: "Di", color: "blue")
    assert_equal "blue", fourth.color
  end

  test "a kicked player's slot is freed for the next joiner" do
    play_session = start_session
    _first, = join_player(play_session, name: "One")
    second, = join_player(play_session, name: "Two")
    join_player(play_session, name: "Three")

    play_session.kick!(second)
    newcomer, = join_player(play_session, name: "Four")
    assert_equal 2, newcomer.slot
  end

  test "a kicked player's seat goes to the longest-waiting connected spectator" do
    play_session = start_session
    seated = 4.times.map { |i| join_player(play_session, name: "P#{i + 1}").first }
    waiting, = join_player(play_session, name: "Waiting")
    online, = join_player(play_session, name: "Online")
    online.mark_connected!("n1")
    assert_nil waiting.slot

    assert_broadcast_on(PlaySessionChannel.broadcasting_for(online), { type: "player_updated", player: online.reload.as_player_json.merge(slot: 3) }) do
      play_session.kick!(seated[2])
    end
    assert_equal 3, online.reload.slot
    assert_nil waiting.reload.slot

    play_session.kick!(seated[0])
    assert_equal 1, waiting.reload.slot
  end

  test "stores only a digest of the token and authenticates the cookie value" do
    player, token = join_player(start_session)

    assert_equal Player.digest_token(token), player.token_digest
    assert_not_includes player.attributes.values.map(&:to_s), token
    assert_equal player, Player.from_cookie(Player.cookie_value(player, token))
    assert_nil Player.from_cookie("#{player.id}:wrong")
    assert_nil Player.from_cookie("garbage")
    assert_nil Player.from_cookie(nil)
  end

  test "validates name and color" do
    play_session = start_session
    assert_raises(ActiveRecord::RecordInvalid) { join_player(play_session, name: "") }
    assert_raises(ActiveRecord::RecordInvalid) { join_player(play_session, name: "x" * 21) }
    assert_raises(ActiveRecord::RecordInvalid) { join_player(play_session, color: "black") }
    assert join_player(play_session, name: "x" * 20, color: "teal").first.persisted?
  end

  test "kicking the host hands host to the lowest connected slot" do
    play_session = start_session
    host, = join_player(play_session, name: "Host")
    second, = join_player(play_session, name: "Two")
    third, = join_player(play_session, name: "Three")
    third.mark_connected!("abc")

    assert_broadcast_on(PlaySessionChannel.broadcasting_for(host), { type: "kicked" }) do
      play_session.kick!(host)
    end

    assert host.reload.kicked?
    assert_not host.host?
    assert third.reload.host?
    assert_not second.reload.host?
  end

  test "kicking the host with nobody connected picks the lowest slot" do
    play_session = start_session
    host, = join_player(play_session, name: "Host")
    second, = join_player(play_session, name: "Two")
    join_player(play_session, name: "Three")

    play_session.kick!(host)
    assert second.reload.host?
  end

  test "host transfer moves the flag and notifies both players and the kiosk" do
    play_session = start_session
    host, = join_player(play_session, name: "Host")
    other, = join_player(play_session, name: "Other")

    assert_broadcasts(PlaySessionChannel.broadcasting_for(play_session), 2) do
      play_session.transfer_host!(other)
    end

    assert_not host.reload.host?
    assert other.reload.host?
    assert_equal 1, play_session.players.where(host: true).count
  end

  test "the first player is also the speaker, and the JSON carries the flag" do
    play_session = start_session
    host, = join_player(play_session, name: "Host")
    other, = join_player(play_session, name: "Other")

    assert host.speaker?
    assert_not other.speaker?
    assert_equal true, host.as_player_json[:speaker]
    assert_equal false, other.as_player_json[:speaker]
  end

  test "speaker transfer moves the flag and notifies both players and the kiosk" do
    play_session = start_session
    host, = join_player(play_session, name: "Host")
    other, = join_player(play_session, name: "Other")

    assert_broadcasts(PlaySessionChannel.broadcasting_for(play_session), 2) do
      assert_broadcasts(PlaySessionChannel.broadcasting_for(other), 1) do
        play_session.transfer_speaker!(other)
      end
    end

    assert other.reload.speaker?
    assert_not host.reload.speaker?
    assert host.host?, "host stays host"
    assert_equal 1, play_session.players.where(speaker: true).count

    assert_no_broadcasts(PlaySessionChannel.broadcasting_for(play_session)) do
      play_session.transfer_speaker!(other)
    end
    assert_raises(ArgumentError) { start_session.transfer_speaker!(other) }
  end

  test "a second speaker pairs as stereo, and either leaving sends the other back to mono" do
    play_session = start_session
    host, = join_player(play_session, name: "Host")
    other, = join_player(play_session, name: "Other")
    third, = join_player(play_session, name: "Third")

    assert_broadcasts(PlaySessionChannel.broadcasting_for(play_session), 2) do
      play_session.pair_speaker!(other, "right")
    end
    assert_equal "left", host.reload.speaker_channel
    assert_equal "right", other.reload.speaker_channel
    assert host.speaker? && other.speaker?
    assert_equal "right", other.as_player_json[:speaker_channel]

    # A third phone asking for the right channel displaces the old one.
    play_session.pair_speaker!(third, "right")
    assert_not other.reload.speaker?
    assert_nil other.speaker_channel
    assert_equal [ "left", "right" ], [ host.reload.speaker_channel, third.reload.speaker_channel ]

    # The right phone leaves: the host plays the whole mix again.
    third.mark_connected!("n")
    play_session.speaker_left!(third)
    assert_not third.reload.speaker?
    assert host.reload.speaker?
    assert_nil host.speaker_channel

    play_session.pair_speaker!(other, "left")
    assert_equal "right", host.reload.speaker_channel
    play_session.kick!(other)
    assert_nil host.reload.speaker_channel
    assert host.speaker?
  end

  test "pairing needs another speaker and a valid channel; be_speaker dissolves a pair" do
    play_session = start_session
    host, = join_player(play_session, name: "Host")
    other, = join_player(play_session, name: "Other")

    assert_raises(ArgumentError) { play_session.pair_speaker!(host, "left") }
    assert_raises(ArgumentError) { play_session.pair_speaker!(other, "middle") }

    play_session.pair_speaker!(other, "left")
    assert_equal "right", host.reload.speaker_channel

    play_session.transfer_speaker!(other)
    assert other.reload.speaker?
    assert_nil other.speaker_channel
    assert_not host.reload.speaker?
    assert_nil host.speaker_channel
  end

  test "kicking the speaker hands the role to the host" do
    play_session = start_session
    host, = join_player(play_session, name: "Host")
    other, = join_player(play_session, name: "Other")
    play_session.transfer_speaker!(other)

    play_session.kick!(other)

    assert_not other.reload.speaker?
    assert host.reload.speaker?
    assert_raises(ArgumentError) { play_session.transfer_speaker!(other) }
  end

  test "kicking a host who is the speaker gives both roles to the new host" do
    play_session = start_session
    host, = join_player(play_session, name: "Host")
    second, = join_player(play_session, name: "Two")
    second.mark_connected!("abc")

    play_session.kick!(host)

    second.reload
    assert second.host?
    assert second.speaker?
    assert_equal 1, play_session.players.active.where(speaker: true).count
  end

  test "a speaker that leaves hands the role to the host; a host speaker keeps it" do
    play_session = start_session
    host, = join_player(play_session, name: "Host")
    other, = join_player(play_session, name: "Other")
    play_session.transfer_speaker!(other)

    play_session.speaker_left!(other.reload)
    assert host.reload.speaker?
    assert_not other.reload.speaker?

    play_session.speaker_left!(host)
    assert host.reload.speaker?
  end

  test "a newcomer becomes the speaker when nobody holds the role" do
    play_session = start_session
    host, = join_player(play_session, name: "Host")
    host.update!(speaker: false)

    newcomer, = join_player(play_session, name: "New")
    assert newcomer.speaker?
  end

  test "a stale subscription can't mark a reconnected player as disconnected" do
    player, = join_player(start_session)
    player.mark_connected!("old")
    player.mark_connected!("new")

    assert_not player.mark_disconnected!("old")
    assert player.reload.connected
    assert player.mark_disconnected!("new")
    assert_not player.reload.connected
  end

  test "avatar is an emoji or a small jpeg or webp selfie" do
    play_session = start_session
    webp = "data:image/webp;base64,#{Base64.strict_encode64("RIFF\x00\x00\x00\x00WEBPVP8 ".b)}"

    [ nil, "\u{1F98A}", "\u{1F1EF}\u{1F1F5}", "\u2764\uFE0F", "\u{1F44B}\u{1F3FD}", jpeg_avatar, webp ].each do |avatar|
      player, = join_player(play_session, avatar: avatar)
      if avatar.nil?
        assert_nil player.avatar
      else
        assert_equal avatar, player.avatar
      end
      play_session.kick!(player)
    end

    assert_nil join_player(play_session, avatar: "  ").first.avatar
  end

  test "rejects avatars that aren't one emoji or a capped jpeg or webp data url" do
    play_session = start_session
    png = "data:image/png;base64,#{Base64.strict_encode64("\x89PNG\r\n".b)}"
    not_jpeg = "data:image/jpeg;base64,#{Base64.strict_encode64("<svg onload=alert(1)>")}"
    svg = "data:image/svg+xml;base64,#{Base64.strict_encode64("<svg/>")}"
    invalid = [
      "A", "1", "AB", "<b>", "\u{1F98A}\u{1F98A}", "\u{1F98A}x", "\u00E9", "\u{1F468}\u200D\u{1F469}\u200D\u{1F467}\u200D\u{1F466}",
      png, svg, not_jpeg, "data:image/jpeg;base64,not base64!", "data:image/jpeg,raw",
      "javascript:alert(1)", jpeg_avatar(bytes: Player::AVATAR_IMAGE_MAX_BYTES + 100)
    ]
    invalid.each do |avatar|
      error = assert_raises(ActiveRecord::RecordInvalid, "accepted #{avatar[0, 40].inspect}") do
        join_player(play_session, avatar: avatar)
      end
      assert error.record.errors.key?(:avatar)
    end
  end

  test "avatar can be a creature, with or without a photo" do
    play_session = start_session
    recipe = "yui:c1:1.0.3.0.2.1.2.4:33-38-13,67-38-13:50-70:50-55"

    [ recipe, "yui:c1:5.7.14.4.3.6.5.5::0-100:100-0", "#{recipe}|#{jpeg_avatar}" ].each do |avatar|
      player, = join_player(play_session, avatar: avatar)
      assert_equal avatar, player.avatar
      play_session.kick!(player)
    end
  end

  test "rejects creatures that are off-format or out of range" do
    play_session = start_session
    recipe = "yui:c1:1.0.3.0.2.1.2.4:33-38-13:50-70:50-55"
    png = "data:image/png;base64,#{Base64.strict_encode64("\x89PNG\r\n".b)}"
    invalid = [
      "yui:c1:", "yui:c2:1.0.3.0.2.1.2.4::50-70:50-55", "yui:c1:6.0.3.0.2.1.2.4::50-70:50-55",
      "yui:c1:1.8.3.0.2.1.2.4::50-70:50-55", "yui:c1:1.0.3.0.2.1.2::50-70:50-55",
      "yui:c1:1.0.3.0.2.1.2.4:1-1-4,1-1-4,1-1-4,1-1-4:50-70:50-55", "yui:c1:1.0.3.0.2.1.2.4:33-38-41:50-70:50-55",
      "yui:c1:1.0.3.0.2.1.2.4:33-38-3:50-70:50-55", "yui:c1:1.0.3.0.2.1.2.4::50-101:50-55",
      "yui:c1:1.0.3.0.2.1.2.4::50-70:50-55:extra", "#{recipe}\n<script>", "#{recipe}|#{png}",
      "#{recipe}|javascript:alert(1)", "#{recipe}|#{jpeg_avatar}|#{jpeg_avatar}",
      "#{recipe}|#{jpeg_avatar(bytes: Player::AVATAR_IMAGE_MAX_BYTES)}"
    ]
    invalid.each do |avatar|
      error = assert_raises(ActiveRecord::RecordInvalid, "accepted #{avatar[0, 60].inspect}") do
        join_player(play_session, avatar: avatar)
      end
      assert error.record.errors.key?(:avatar)
    end
  end

  test "the player json carries the avatar" do
    player, = join_player(start_session, avatar: "\u{1F98A}")
    assert_equal "\u{1F98A}", player.as_player_json[:avatar]
    player.update!(avatar: nil)
    assert player.as_player_json.key?(:avatar)
    assert_nil player.as_player_json[:avatar]
  end
end
