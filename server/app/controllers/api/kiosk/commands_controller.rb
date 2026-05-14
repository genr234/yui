module Api
  module Kiosk
    class CommandsController < BaseController
      def index
        touch_current_kiosk!
        commands = current_kiosk.kiosk_commands.deliverable.limit(25)
        commands.each(&:mark_sent!)
        render json: { commands: commands.map { |command| command_payload(command) } }
      end

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
        render json: { command: command_payload(command) }
      end

      private

      def command_payload(command)
        {
          id: command.id.to_s,
          command_type: command.command_type,
          payload: command.payload,
          status: command.status
        }
      end
    end
  end
end
