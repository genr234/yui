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
end
