class KioskCommandsChannel < ApplicationCable::Channel
  def subscribed
    current_kiosk.touch_seen!
    stream_for current_kiosk
    current_kiosk.kiosk_commands.deliverable.limit(25).each(&:deliver_over_websocket!)
  end
end
