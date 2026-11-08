# WebRTC signaling relay between a kiosk and the players of its play session.
#
# Kiosk:  subscribe { channel: "PlaySessionChannel", session_id }, streams the session.
# Player: subscribe { channel: "PlaySessionChannel" }, streams its own player record.
#
# Kiosk -> player: perform "signal", { to, type: offer|ice|bye|control, data } -> { type, data }
# Player -> kiosk: perform "signal", { type: answer|ice|hello|control, data } -> { from, type, data }
# "control" is the ICE-failure fallback: data is one §3 control object.
# Anything else is dropped.
class PlaySessionChannel < ApplicationCable::Channel
  KIOSK_SIGNAL_TYPES = %w[offer ice bye control].freeze
  PLAYER_SIGNAL_TYPES = %w[answer ice hello control].freeze
  MAX_SIGNAL_BYTES = 64.kilobytes

  def subscribed
    if current_kiosk
      subscribe_kiosk
    elsif current_player
      subscribe_player
    else
      reject
    end
  end

  def unsubscribed
    return unless @player && @connection_nonce

    left = @player.mark_disconnected!(@connection_nonce)
    return unless left && !@player.kicked?

    PlaySessionChannel.broadcast_to(@player.play_session, { type: "player_left", player: @player.as_player_json })
  end

  def signal(payload)
    data = payload["data"].nil? ? {} : payload["data"]
    return unless data.is_a?(Hash) && data.to_json.bytesize <= MAX_SIGNAL_BYTES

    if @play_session && current_kiosk
      relay_from_kiosk(payload, data)
    elsif @player
      relay_from_player(payload, data)
    end
  end

  private

  def subscribe_kiosk
    @play_session = current_kiosk.play_sessions.active.find_by(id: params[:session_id])
    return reject unless @play_session

    current_kiosk.touch_seen!
    stream_for @play_session
  end

  def subscribe_player
    @player = Player.find_by(id: current_player.id)
    return reject unless @player&.playable?

    @connection_nonce = SecureRandom.hex(8)
    @player.mark_connected!(@connection_nonce)
    stream_for @player
    PlaySessionChannel.broadcast_to(@player.play_session, { type: "player_joined", player: @player.as_player_json })
  end

  def relay_from_kiosk(payload, data)
    type = payload["type"].to_s
    return unless KIOSK_SIGNAL_TYPES.include?(type)
    return unless @play_session.reload.active?

    player = @play_session.players.active.find_by(id: payload["to"].to_s)
    return unless player

    PlaySessionChannel.broadcast_to(player, { type: type, data: data })
  end

  def relay_from_player(payload, data)
    type = payload["type"].to_s
    return unless PLAYER_SIGNAL_TYPES.include?(type)
    return unless @player.reload.playable?

    PlaySessionChannel.broadcast_to(@player.play_session, { from: @player.id.to_s, type: type, data: data })
  end
end
