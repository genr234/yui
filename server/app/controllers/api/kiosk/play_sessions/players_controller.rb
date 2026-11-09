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
          # With `channel` (left|right) the player joins the current speaker as
          # a stereo pair; without it the player becomes the one mono speaker.
          if params[:channel].present?
            @play_session.pair_speaker!(@player, params[:channel].to_s)
          else
            @play_session.transfer_speaker!(@player)
          end
          head :no_content
        rescue ArgumentError => error
          render json: { error: "invalid", detail: error.message }, status: :unprocessable_entity
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
