require "test_helper"

class PlaySessionChannelTest < ActionCable::Channel::TestCase
  include PlaySessionTestHelper

  setup do
    @kiosk = create_kiosk
    @play_session = start_session(@kiosk)
    @player, = join_player(@play_session)
  end

  def kiosk_stream
    PlaySessionChannel.broadcasting_for(@play_session)
  end

  def player_stream(player = @player)
    PlaySessionChannel.broadcasting_for(player)
  end

  test "the kiosk subscribes to its own active session" do
    stub_connection current_kiosk: @kiosk, current_player: nil
    subscribe session_id: @play_session.id

    assert subscription.confirmed?
    assert_has_stream kiosk_stream
    assert @kiosk.reload.last_seen_at.present?, "subscribing counts as kiosk activity"
  end

  test "the kiosk can't subscribe to another kiosk's or an ended session" do
    stub_connection current_kiosk: create_kiosk, current_player: nil
    subscribe session_id: @play_session.id
    assert subscription.rejected?

    stub_connection current_kiosk: @kiosk, current_player: nil
    subscribe
    assert subscription.rejected?

    @play_session.end!
    subscribe session_id: @play_session.id
    assert subscription.rejected?
  end

  test "a player subscribes without params, is marked connected and announced to the kiosk" do
    stub_connection current_kiosk: nil, current_player: @player

    assert_broadcasts(kiosk_stream, 1) { subscribe }
    assert subscription.confirmed?
    assert_has_stream player_stream
    assert @player.reload.connected

    assert_broadcast_on(kiosk_stream, { type: "player_left", player: @player.reload.as_player_json.merge(connected: false) }) do
      unsubscribe
    end
    assert_not @player.reload.connected
  end

  test "when the speaker's phone leaves, the host becomes the speaker" do
    other, = join_player(@play_session, name: "Other")
    @play_session.transfer_speaker!(other)
    stub_connection current_kiosk: nil, current_player: other
    subscribe

    # player_left, then player_updated for the old speaker and the host.
    assert_broadcasts(kiosk_stream, 3) { unsubscribe }
    assert @player.reload.speaker?
    assert_not other.reload.speaker?
  end

  test "kicked players and players of ended sessions are rejected" do
    kicked, = join_player(@play_session, name: "Kicked")
    @play_session.kick!(kicked)
    stub_connection current_kiosk: nil, current_player: kicked
    subscribe
    assert subscription.rejected?

    stub_connection current_kiosk: nil, current_player: @player
    @play_session.end!
    subscribe
    assert subscription.rejected?
  end

  test "kiosk signals are routed to the addressed player" do
    stub_connection current_kiosk: @kiosk, current_player: nil
    subscribe session_id: @play_session.id

    assert_broadcast_on(player_stream, { type: "offer", data: { "sdp" => "v=0" } }) do
      perform :signal, to: @player.id.to_s, type: "offer", data: { sdp: "v=0" }
    end
  end

  test "kiosk signals with a bad type, foreign or kicked player are dropped" do
    other_session = start_session
    stranger, = join_player(other_session)
    kicked, = join_player(@play_session, name: "Kicked")
    @play_session.kick!(kicked)

    stub_connection current_kiosk: @kiosk, current_player: nil
    subscribe session_id: @play_session.id

    assert_no_broadcasts(player_stream) { perform :signal, to: @player.id.to_s, type: "answer", data: {} }
    assert_no_broadcasts(player_stream) { perform :signal, to: @player.id.to_s, type: "offer", data: "not a hash" }
    assert_no_broadcasts(player_stream(stranger)) { perform :signal, to: stranger.id.to_s, type: "offer", data: {} }
    assert_no_broadcasts(player_stream(kicked)) { perform :signal, to: kicked.id.to_s, type: "offer", data: {} }
  end

  test "player signals are routed to the kiosk with the sender's id" do
    stub_connection current_kiosk: nil, current_player: @player
    subscribe

    assert_broadcast_on(kiosk_stream, { from: @player.id.to_s, type: "answer", data: { "sdp" => "v=0" } }) do
      perform :signal, type: "answer", data: { sdp: "v=0" }
    end
    assert_broadcast_on(kiosk_stream, { from: @player.id.to_s, type: "hello", data: {} }) do
      perform :signal, type: "hello"
    end
  end

  test "control fallback frames are relayed both ways" do
    frame = { "t" => "ping", "id" => 1, "ts" => 42 }

    stub_connection current_kiosk: @kiosk, current_player: nil
    subscribe session_id: @play_session.id
    assert_broadcast_on(player_stream, { type: "control", data: frame }) do
      perform :signal, to: @player.id.to_s, type: "control", data: frame
    end

    stub_connection current_kiosk: nil, current_player: @player
    subscribe
    assert_broadcast_on(kiosk_stream, { from: @player.id.to_s, type: "control", data: frame }) do
      perform :signal, type: "control", data: frame
    end
  end

  test "player signals with a bad type, or after a kick, are dropped" do
    stub_connection current_kiosk: nil, current_player: @player
    subscribe

    assert_no_broadcasts(kiosk_stream) { perform :signal, type: "offer", data: {} }
    assert_no_broadcasts(kiosk_stream) { perform :signal, type: "bye", data: {} }

    @play_session.kick!(@player)
    assert_no_broadcasts(kiosk_stream) { perform :signal, type: "answer", data: {} }
  end

  test "a player can't address another player or a different session" do
    other, = join_player(@play_session, name: "Other")
    stub_connection current_kiosk: nil, current_player: @player
    subscribe

    assert_no_broadcasts(player_stream(other)) do
      perform :signal, to: other.id.to_s, type: "ice", data: {}
    end
  end
end
