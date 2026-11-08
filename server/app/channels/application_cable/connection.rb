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

    # Production rejects a missing or foreign Origin. Browsers always send one,
    # and a page cannot set Authorization on a WebSocket, so a foreign Origin is
    # never treated as the kiosk. The Go client sends the server's own Origin.
    # A missing Origin is not a browser; allow it only after the bearer token
    # authenticates a kiosk. Any "Bearer " prefix must not skip the check, or an
    # invalid token plus a player cookie could connect from another site.
    def allow_request_origin?
      return true if request.get_header("HTTP_ORIGIN").blank? && find_verified_kiosk
      super
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
