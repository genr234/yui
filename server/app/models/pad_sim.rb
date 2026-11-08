# The Yui Pad simulator (/sim): up to four pads in iframes of one browser,
# for developing without phones. Development and test only.
#
# Every iframe loads /play/:code?sim=N and the pad repeats `sim=N` on its
# requests and its /cable URL, so each simulated phone gets its own player
# cookie (`yui_player_sim<N>`) instead of all four sharing `yui_player`.
# In production `sim` is ignored everywhere.
module PadSim
  SLOTS = (1..4)

  def self.enabled?
    Rails.env.local?
  end

  # 1..4 when the request is a simulated pad, nil otherwise (and always in production).
  def self.slot(param)
    return unless enabled?

    value = param.to_s
    value.to_i if value.match?(/\A[1-4]\z/)
  end

  def self.cookie_name(param)
    n = slot(param)
    n ? :"yui_player_sim#{n}" : PlayController::PLAYER_COOKIE
  end
end
