require "test_helper"

class IceConfigTest < ActiveSupport::TestCase
  NOW = Time.utc(2026, 1, 1)

  test "returns STUN and coturn REST credentials with a verifiable HMAC" do
    env = {
      "TURN_URLS" => "turn:turn.example.com:3478?transport=udp, turns:turn.example.com:5349",
      "TURN_SECRET" => "s3cret",
      "STUN_URLS" => "stun:turn.example.com:3478"
    }
    config = IceConfig.new(env: env, now: NOW).as_json_for("p42")

    assert_equal 86_400, config[:ttl]
    stun, turn = config[:ice_servers]
    assert_equal({ urls: [ "stun:turn.example.com:3478" ] }, stun)
    assert_equal [ "turn:turn.example.com:3478?transport=udp", "turns:turn.example.com:5349" ], turn[:urls]

    expiry, suffix = turn[:username].split(":")
    assert_equal NOW.to_i + 86_400, expiry.to_i
    assert_equal "p42", suffix
    expected = Base64.strict_encode64(OpenSSL::HMAC.digest("SHA1", "s3cret", turn[:username]))
    assert_equal expected, turn[:credential]
  end

  test "uses k/p suffixes for kiosks and players and honours TURN_TTL" do
    env = { "TURN_URLS" => "turn:t:3478", "TURN_SECRET" => "x", "TURN_TTL" => "600" }
    kiosk = Kiosk.new(id: 7)
    player = Player.new(id: 9)

    assert_match(/\A\d+:k7\z/, IceConfig.for_kiosk(kiosk, env: env)[:ice_servers].last[:username])
    assert_match(/\A\d+:p9\z/, IceConfig.for_player(player, env: env)[:ice_servers].last[:username])
    assert_equal 600, IceConfig.for_kiosk(kiosk, env: env)[:ttl]
  end

  test "returns only the default STUN server without a TURN secret" do
    config = IceConfig.new(env: { "TURN_URLS" => "turn:t:3478" }, now: NOW).as_json_for("k1")

    assert_equal [ { urls: [ "stun:stun.l.google.com:19302" ] } ], config[:ice_servers]
  end
end
