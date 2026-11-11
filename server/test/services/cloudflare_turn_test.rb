require "test_helper"

class CloudflareTurnTest < ActiveSupport::TestCase
  ENV_KEYS = { "CLOUDFLARE_TURN_KEY_ID" => "key123", "CLOUDFLARE_TURN_API_TOKEN" => "tok" }.freeze

  RESPONSE = {
    iceServers: [
      { urls: [ "stun:stun.cloudflare.com:3478", "stun:stun.cloudflare.com:53" ] },
      {
        urls: [
          "turn:turn.cloudflare.com:3478?transport=udp", "turn:turn.cloudflare.com:53?transport=udp",
          "turn:turn.cloudflare.com:3478?transport=tcp", "turns:turn.cloudflare.com:5349?transport=tcp",
          "turns:turn.cloudflare.com:443?transport=tcp"
        ],
        username: "u1", credential: "c1"
      }
    ]
  }.to_json

  test "asks Cloudflare for credentials with the key and drops port 53" do
    calls = []
    http = ->(uri, headers, body) { calls << [ uri, headers, body ]; [ "201", RESPONSE ] }
    config = IceConfig.new(env: ENV_KEYS, now: Time.utc(2026, 1, 1), cloudflare: CloudflareTurn.new(env: ENV_KEYS, http: http))
      .as_json_for("p1")

    uri, headers, body = calls.sole
    assert_equal "https://rtc.live.cloudflare.com/v1/turn/keys/key123/credentials/generate-ice-servers", uri.to_s
    assert_equal "Bearer tok", headers["Authorization"]
    assert_equal({ "ttl" => 86_400 }, JSON.parse(body))

    stun, turn = config[:ice_servers]
    assert_equal [ "stun:stun.cloudflare.com:3478" ], stun[:urls]
    assert_not turn[:urls].any? { |url| url.match?(/:53(\z|\?)/) }
    assert_equal 4, turn[:urls].size
    assert_equal [ "u1", "c1" ], [ turn[:username], turn[:credential] ]
    assert_equal 86_400, config[:ttl]
  end

  test "falls back to STUN when Cloudflare fails" do
    [ ->(*) { [ "500", "{}" ] }, ->(*) { raise Net::OpenTimeout } ].each do |http|
      config = IceConfig.new(env: ENV_KEYS, cloudflare: CloudflareTurn.new(env: ENV_KEYS, http: http)).as_json_for("k1")
      assert_equal [ { urls: [ "stun:stun.l.google.com:19302" ] } ], config[:ice_servers]
    end
  end

  test "is off without both settings" do
    assert CloudflareTurn.configured?(ENV_KEYS)
    assert_not CloudflareTurn.configured?(ENV_KEYS.except("CLOUDFLARE_TURN_API_TOKEN"))
  end
end
