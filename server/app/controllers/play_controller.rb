# Phone-facing endpoints for Yui Pad. No user login: phones are identified by
# the signed `yui_player` cookie set on join.
#
# Inherits from ActionController::Base (not ApplicationController) so the
# control-room login, the `allow_browser` gate and the app layout don't apply.
#
# CSRF: the only state-changing endpoint is POST /play/:code/join, which the
# static pad app calls with fetch(). The pad is a static file (no Rails-rendered
# meta tag) and the shared contract has no token field, so instead of the Rails
# authenticity token we require all of:
#   * `Content-Type: application/json` (a cross-site HTML form can't send it,
#     and a cross-origin fetch with it needs a CORS preflight we never answer),
#   * a same-origin `Origin` / `Sec-Fetch-Site` header when the browser sends one,
#   * the player cookie being SameSite=Lax (never sent on cross-site POSTs).
class PlayController < ActionController::Base
  PLAYER_COOKIE = :yui_player
  COOKIE_LIFETIME = 1.day
  class_attribute :pad_index, default: Rails.public_path.join("pad", "index.html")

  skip_forgery_protection only: :join

  rate_limit to: 10, within: 1.minute, only: :join,
    with: -> { render json: { error: "rate_limited" }, status: :too_many_requests }

  before_action :verify_same_origin_json!, only: :join

  # /sim shows pads in same-origin iframes; everywhere else the pad stays unframeable.
  content_security_policy(only: :show, if: :sim_request?) do |policy|
    policy.frame_ancestors :self
  end
  before_action :set_play_session

  def show
    response.headers["Cache-Control"] = "no-cache"
    if pad_index.file?
      send_file pad_index, type: "text/html", disposition: "inline"
    else
      render :placeholder, layout: false
    end
  end

  def info
    player = cookie_player
    render json: {
      code: @play_session.code,
      kiosk_name: @play_session.kiosk.name,
      lobby_open: @play_session.lobby_open,
      player: player&.as_player_json,
      colors_taken: @play_session.players.active.where.not(id: player&.id).distinct.pluck(:color)
    }
  end

  def join
    player = cookie_player(include_kicked: true)
    return render json: { error: "kicked" }, status: :forbidden if player&.kicked?

    if player
      rejoin(player)
    elsif @play_session.lobby_open || @play_session.valid_qr_token?(params[:qr_token])
      first_join
    else
      render json: { error: "qr_required" }, status: :forbidden
    end
  end

  def ice
    player = cookie_player
    return render json: { error: "not_joined" }, status: :unauthorized unless player

    render json: IceConfig.for_player(player)
  end

  private

  def set_play_session
    @play_session = PlaySession.find_active_by_code(params[:code])
    return if @play_session

    if action_name == "show"
      render file: Rails.public_path.join("404.html"), status: :not_found, layout: false, content_type: "text/html"
    else
      render json: { error: "not_found" }, status: :not_found
    end
  end

  def first_join
    player, token = Player.join!(@play_session, name: params[:name], color: params[:color])
    write_player_cookie(player, token)
    render_joined(player)
  rescue ActiveRecord::RecordInvalid => error
    render_invalid(error.record)
  end

  # Rejoin keeps the slot; name and color may change.
  def rejoin(player)
    player.name = params[:name] if params.key?(:name)
    player.color = @play_session.free_color(params[:color], except: player) if params.key?(:color)
    changed = player.changed?
    return render_invalid(player) unless player.save

    player.broadcast_updated if changed
    render_joined(player)
  end

  def render_joined(player)
    render json: {
      player: player.as_player_json,
      session: { id: @play_session.id.to_s, code: @play_session.code }
    }
  end

  def render_invalid(record)
    render json: { error: "validation_failed", details: record.errors.to_hash }, status: :unprocessable_content
  end

  # The cookie's player, only if it belongs to this session.
  def cookie_player(include_kicked: false)
    player = Player.from_cookie(cookies.signed[player_cookie_name])
    return unless player && player.play_session_id == @play_session.id
    return if player.kicked? && !include_kicked

    player
  end

  # `yui_player`, or a per-pad cookie for /sim iframes (development and test only, see PadSim).
  def player_cookie_name
    PadSim.cookie_name(params[:sim])
  end

  def sim_request?
    PadSim.slot(params[:sim]).present?
  end

  def write_player_cookie(player, token)
    cookies.signed[player_cookie_name] = {
      value: Player.cookie_value(player, token),
      expires: COOKIE_LIFETIME.from_now,
      httponly: true,
      same_site: :lax,
      secure: Rails.env.production?
    }
  end

  def verify_same_origin_json!
    unless request.media_type == "application/json"
      return render json: { error: "unsupported_media_type" }, status: :unsupported_media_type
    end

    origin = request.headers["Origin"]
    fetch_site = request.headers["Sec-Fetch-Site"]
    cross_origin = origin.present? && origin != request.base_url
    cross_site_fetch = fetch_site.present? && !%w[same-origin none].include?(fetch_site)
    render json: { error: "forbidden_origin" }, status: :forbidden if cross_origin || cross_site_fetch
  end
end
