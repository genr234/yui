class ApplicationController < ActionController::Base
  # Only allow modern browsers supporting webp images, web push, badges, import maps, CSS nesting, and CSS :has.
  allow_browser versions: :modern

  # Changes to the importmap will invalidate the etag for HTML responses
  stale_when_importmap_changes

  before_action :require_control_room_access

  helper_method :current_user, :signed_in?

  private

  def current_user
    @current_user ||= User.find_by(id: session[:user_id]) if session[:user_id].present?
  end

  def signed_in?
    current_user.present?
  end

  def sign_in(user)
    reset_session
    session[:user_id] = user.id
    mark_recently_authenticated
  end

  def mark_recently_authenticated
    session[:authenticated_at] = Time.current.to_i
  end

  def sign_out
    reset_session
  end

  def require_control_room_access
    return if controller_path.start_with?("api/")
    return if controller_name == "sessions"
    return if controller_name == "passkeys"
    return if request.path == rails_health_check_path

    if User.exists?
      require_authentication
    else
      redirect_to setup_path unless request.path == setup_path
    end
  end

  def require_authentication
    return if signed_in?

    remember_return_to
    redirect_to login_path, alert: "Please log in first."
  end

  def require_recent_authentication
    return if recently_authenticated?

    remember_return_to
    redirect_to login_path, alert: "Confirm your passkey before making that change."
  end

  def recently_authenticated?
    signed_in? && session[:authenticated_at].to_i > 10.minutes.ago.to_i
  end

  def remember_return_to
    session[:return_to] = request.fullpath if request.get? || request.head?
  end

  def after_authentication_path
    session.delete(:return_to).presence || accounts_path
  end
end
