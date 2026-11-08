module Api
  module Kiosk
    module PlaySessions
      class PlayersController < BaseController
        before_action :set_player

        def kick
          @play_session.kick!(@player)
          head :no_content
        end

        def host
          @play_session.transfer_host!(@player)
          head :no_content
        end

        def speaker
          @play_session.transfer_speaker!(@player)
          head :no_content
        end

        private

        def set_player
          @play_session = current_kiosk.play_sessions.active.find_by(id: params[:play_session_id])
          @player = @play_session&.players&.active&.find_by(id: params[:player_id])
          render json: { error: "not_found" }, status: :not_found unless @player
        end
      end
    end
  end
end
