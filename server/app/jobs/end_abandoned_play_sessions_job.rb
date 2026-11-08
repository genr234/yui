# Ends play sessions whose kiosk has gone quiet for PlaySession::KIOSK_TIMEOUT
# (crash, power cut). end! tells the players `session_ended` and closes their sockets.
class EndAbandonedPlaySessionsJob < ApplicationJob
  queue_as :default

  def perform
    PlaySession.abandoned.find_each(&:end!)
  end
end
