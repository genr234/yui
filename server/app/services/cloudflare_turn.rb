require "net/http"

# Short-lived TURN credentials from Cloudflare's TURN service (Realtime).
# Cloudflare answers from its nearest location, so the relay sits close to
# both the kiosk and the phone. The TURN key stays here on the server; each
# kiosk and phone gets its own credential that expires after ttl seconds.
#
#   CLOUDFLARE_TURN_KEY_ID     the TURN key's id (dashboard: Realtime > TURN)
#   CLOUDFLARE_TURN_API_TOKEN  that key's API token
#
# Docs: https://developers.cloudflare.com/realtime/turn/generate-credentials/
class CloudflareTurn
  ENDPOINT = "https://rtc.live.cloudflare.com/v1/turn/keys/%s/credentials/generate-ice-servers".freeze
  TIMEOUT = 3

  def self.configured?(env)
    env["CLOUDFLARE_TURN_KEY_ID"].present? && env["CLOUDFLARE_TURN_API_TOKEN"].present?
  end

  # http is called as http.call(uri, headers, body) and returns
  # [status, body]; tests pass their own.
  def initialize(env:, http: method(:post))
    @key_id = env["CLOUDFLARE_TURN_KEY_ID"]
    @token = env["CLOUDFLARE_TURN_API_TOKEN"]
    @http = http
  end

  # The ice servers (STUN plus TURN with a fresh username and credential),
  # or nil when Cloudflare can't be reached: the caller then hands out
  # STUN only and the pad's direct and fallback paths still work.
  def ice_servers(ttl)
    status, body = @http.call(
      URI(format(ENDPOINT, @key_id)),
      { "Authorization" => "Bearer #{@token}", "Content-Type" => "application/json" },
      { ttl: ttl }.to_json
    )
    unless status.to_i.between?(200, 299)
      Rails.logger.warn("cloudflare turn: HTTP #{status}")
      return nil
    end
    servers = Array(JSON.parse(body)["iceServers"])
    servers.map { |server| without_port_53(server) }.reject { |server| server["urls"].empty? }.map(&:symbolize_keys)
  rescue StandardError => error
    Rails.logger.warn("cloudflare turn: #{error.class}: #{error.message}")
    nil
  end

  private

  # Browsers block port 53, so those URLs only make a phone wait for a
  # timeout (Cloudflare's docs say to drop them).
  def without_port_53(server)
    urls = Array(server["urls"]).reject { |url| url.match?(/:53(\z|\?)/) }
    server.merge("urls" => urls)
  end

  def post(uri, headers, body)
    response = Net::HTTP.start(uri.host, uri.port, use_ssl: true, open_timeout: TIMEOUT, read_timeout: TIMEOUT) do |http|
      http.post(uri.path, body, headers)
    end
    [ response.code, response.body ]
  end
end
