# ICE servers for a WebRTC peer, with coturn REST API ("use-auth-secret") credentials:
#   username   = "<expiry unix>:<k|p><id>"
#   credential = Base64(HMAC-SHA1(TURN_SECRET, username))
# Without TURN_SECRET (or TURN_URLS) only STUN servers are returned.
class IceConfig
  DEFAULT_STUN_URLS = "stun:stun.l.google.com:19302".freeze
  DEFAULT_TTL = 86_400

  def self.for_kiosk(kiosk, **options)
    new(**options).as_json_for("k#{kiosk.id}")
  end

  def self.for_player(player, **options)
    new(**options).as_json_for("p#{player.id}")
  end

  def initialize(env: ENV, now: Time.current)
    @env = env
    @now = now
  end

  def ttl
    value = @env["TURN_TTL"].to_i
    value.positive? ? value : DEFAULT_TTL
  end

  def stun_urls
    list(@env.fetch("STUN_URLS", DEFAULT_STUN_URLS))
  end

  def turn_urls
    list(@env["TURN_URLS"])
  end

  def turn_secret
    @env["TURN_SECRET"].presence
  end

  def as_json_for(suffix)
    servers = []
    servers << { urls: stun_urls } if stun_urls.any?

    if turn_secret && turn_urls.any?
      username = "#{@now.to_i + ttl}:#{suffix}"
      servers << { urls: turn_urls, username: username, credential: self.class.credential(turn_secret, username) }
    end

    { ice_servers: servers, ttl: ttl }
  end

  def self.credential(secret, username)
    Base64.strict_encode64(OpenSSL::HMAC.digest("SHA1", secret, username))
  end

  private

  def list(value)
    value.to_s.split(",").map(&:strip).reject(&:blank?)
  end
end
