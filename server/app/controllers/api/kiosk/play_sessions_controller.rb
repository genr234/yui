module Api
  module Kiosk
    class PlaySessionsController < BaseController
      before_action :set_play_session, only: [ :update, :destroy, :ice ]

      def create
        touch_current_kiosk!
        play_session, created = PlaySession.find_or_start_for!(current_kiosk)
        render json: play_session.as_kiosk_json(base_url: request.base_url), status: created ? :created : :ok
      end

      def update
        if params.key?(:lobby_open)
          @play_session.update!(lobby_open: ActiveModel::Type::Boolean.new.cast(params[:lobby_open]) || false)
        end
        render json: @play_session.as_kiosk_json(base_url: request.base_url)
      end

      def destroy
        @play_session.end!
        head :no_content
      end

      def ice
        render json: IceConfig.for_kiosk(current_kiosk)
      end

      private

      # Only the kiosk's own, still active session.
      def set_play_session
        @play_session = current_kiosk.play_sessions.active.find_by(id: params[:id])
        render json: { error: "not_found" }, status: :not_found unless @play_session
      end
    end
  end
end
