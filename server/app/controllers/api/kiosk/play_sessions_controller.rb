module Api
  module Kiosk
    class PlaySessionsController < BaseController
      before_action :set_play_session, only: [ :show, :update, :destroy, :ice ]

      def create
        touch_current_kiosk!
        play_session, created = PlaySession.find_or_start_for!(current_kiosk)
        render json: play_session.as_kiosk_json(base_url: request.base_url), status: created ? :created : :ok
      end

      # The current roster, so the kiosk can resync after a signaling outage.
      def show
        @play_session.release_stale_players!
        render json: @play_session.as_kiosk_json(base_url: request.base_url)
      end

      # Nothing to change any more: `lobby_open` from older controllers is
      # accepted and ignored, since every session is joinable with its code.
      def update
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
