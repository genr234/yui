module Api
  module Kiosk
    class BaseController < ActionController::API
      before_action :authenticate_kiosk!

      attr_reader :current_kiosk

      private

      def authenticate_kiosk!
        token = request.authorization.to_s.delete_prefix("Bearer ").presence
        @current_kiosk = ::Kiosk.authenticate(token) if token
        return if @current_kiosk

        render json: { error: "unauthorized" }, status: :unauthorized
      end

      def current_account
        current_kiosk.account
      end

      def touch_current_kiosk!
        current_kiosk.touch_seen!
      end
    end
  end
end
