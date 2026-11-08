# Closes a kicked player's (or ended session's player's) ActionCable connections
# for good. It runs a moment after the `kicked` / `session_ended` broadcast: a
# remote disconnect is handled at once, while stream messages go through the
# cable worker pool, so closing immediately could drop that last message.
# Nothing happens if the player has no open connection.
class DisconnectPlayerJob < ApplicationJob
  DELAY = 2.seconds

  queue_as :default
  discard_on ActiveJob::DeserializationError

  def perform(player)
    ActionCable.server.remote_connections
      .where(current_kiosk: nil, current_player: player)
      .disconnect(reconnect: false)
  end
end
