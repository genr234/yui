# /sim: four Yui Pads side by side for one play session, for development
# without phones. The route only exists in development and test, and the
# action refuses to run anywhere else as well.
#
# Each iframe is /play/:code?sim=N. The pad then uses a per-pad player cookie
# and replaces DeviceMotion with a mouse-drag "fake gyro" (see PadSim and
# pad/src/platform/motion.ts SimMotionSource). The page itself has no script.
class PadSimController < ActionController::Base
  before_action { head :not_found unless PadSim.enabled? }

  def show
    @code = params[:code].to_s.upcase.gsub(/[^A-Z]/, "")[0, 4]
    @qr_token = params[:t].to_s.gsub(/[^A-Z2-7]/i, "")[0, 16].presence
    @count = params[:pads].to_s.match?(/\A[1-4]\z/) ? params[:pads].to_i : 4
    @play_session = PlaySession.find_active_by_code(@code) if @code.length == 4
    @active_sessions = PlaySession.active.includes(:kiosk).order(created_at: :desc).limit(5) unless @play_session
    render layout: false
  end
end
