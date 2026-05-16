module Api
  module Kiosk
    class CommandsController < BaseController
      def update
        touch_current_kiosk!
        command = current_kiosk.kiosk_commands.find(params[:id])
        status = params.require(:status)
        unless %w[succeeded failed].include?(status)
          return render json: { error: "invalid status" }, status: :unprocessable_content
        end

        command.update!(
          status: status,
          result: params[:result],
          error: params[:error],
          completed_at: Time.current
        )
        render json: { command: command.websocket_payload }
      end
    end
  end
end
