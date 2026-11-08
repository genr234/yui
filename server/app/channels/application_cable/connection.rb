module ApplicationCable
  # A connection is either a kiosk (Bearer device token) or a Yui Pad player
  # (signed `yui_player` cookie). Anything else is rejected.
  class Connection < ActionCable::Connection::Base
    identified_by :current_kiosk, :current_player

    def connect
      self.current_kiosk = find_verified_kiosk
      self.current_player = find_verified_player unless current_kiosk
      reject_unauthorized_connection unless current_kiosk || current_player
    end

    private

    def find_verified_kiosk
      token = request.authorization.to_s.delete_prefix("Bearer ").presence
      Kiosk.authenticate(token) if token
    end

    def find_verified_player
      player = Player.from_cookie(cookies.signed[PlayController::PLAYER_COOKIE])
      player if player&.playable?
    end
  end
end
