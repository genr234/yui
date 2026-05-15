module ApplicationCable
  class Connection < ActionCable::Connection::Base
    identified_by :current_kiosk

    def connect
      self.current_kiosk = find_verified_kiosk
    end

    private

    def find_verified_kiosk
      token = request.authorization.to_s.delete_prefix("Bearer ").presence
      kiosk = Kiosk.authenticate(token) if token
      return kiosk if kiosk

      reject_unauthorized_connection
    end
  end
end
