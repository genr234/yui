# A kiosk's page in the control room: health from its telemetry, one-click
# maintenance (restart Chrome, update now) and remote control of its screen.
# Everything goes to the kiosk as a KioskCommand over its command channel.
class KiosksController < ApplicationController
  before_action :require_recent_authentication
  before_action :set_kiosk

  REMOTE_TYPES = %w[kiosk.screenshot kiosk.input].freeze

  def show
    @events = @kiosk.kiosk_events.recent.limit(60)
    @commands = @kiosk.kiosk_commands.where.not(command_type: REMOTE_TYPES).order(created_at: :desc).limit(8)
  end

  def restart_chrome
    queue("kiosk.restartChrome", notice: "Asked #{@kiosk.name} to restart Chrome.")
  end

  def update_now
    queue("update.apply", notice: "Asked #{@kiosk.name} to install the latest release. It restarts if there is one.")
  end

  def check_update
    queue("update.check", notice: "Asked #{@kiosk.name} to check for updates; the result shows under Recent actions.")
  end

  def remote; end

  # JSON for the remote screen: queue a command, then poll remote_command.
  def remote_screenshot
    prune_remote_commands
    command = @kiosk.kiosk_commands.create!(command_type: "kiosk.screenshot", payload: { max_side: params[:max_side].to_i.clamp(320, 1600) })
    render json: { id: command.id.to_s }
  end

  def remote_input
    action = params[:action_type].to_s
    payload = { action: action }
    case action
    when "tap", "scroll"
      payload[:x] = params[:x].to_f.clamp(0.0, 1.0)
      payload[:y] = params[:y].to_f.clamp(0.0, 1.0)
      payload[:dy] = params[:dy].to_f.clamp(-5000.0, 5000.0) if action == "scroll"
    when "key" then payload[:key] = params[:key].to_s.first(16)
    when "text" then payload[:text] = params[:text].to_s.first(500)
    else return render(json: { error: "unknown action" }, status: :unprocessable_content)
    end
    command = @kiosk.kiosk_commands.create!(command_type: "kiosk.input", payload: payload)
    render json: { id: command.id.to_s }
  end

  def remote_command
    command = @kiosk.kiosk_commands.where(command_type: REMOTE_TYPES).find(params[:command_id])
    render json: { status: command.status, result: command.result, error: command.error, online: @kiosk.online? }
  end

  private

  def set_kiosk
    @account = Account.find(params[:account_id])
    @kiosk = @account.kiosks.find(params[:id])
  end

  def queue(type, notice:)
    @kiosk.kiosk_commands.create!(command_type: type, payload: {})
    redirect_to account_kiosk_path(@account, @kiosk), notice: @kiosk.online? ? notice : "#{notice} #{@kiosk.name} is offline, so it runs when it reconnects."
  end

  # Screenshots are big and only useful for a moment.
  def prune_remote_commands
    @kiosk.kiosk_commands.where(command_type: REMOTE_TYPES).where(created_at: ...2.minutes.ago).delete_all
  end
end
