require "test_helper"

class PadSimTest < ActionDispatch::IntegrationTest
  include PlaySessionTestHelper

  setup do
    PlayController.cache_store.clear
    @play_session = start_session(lobby_open: true)
  end

  test "GET /sim lists active sessions when no code is given" do
    get "/sim"

    assert_response :success
    assert_includes response.body, @play_session.code
    assert_no_match(/<script/i, response.body)
  end

  test "GET /sim?code= shows one iframe per simulated pad" do
    get "/sim", params: { code: @play_session.code.downcase, pads: 3, t: "ABCDEFGH" }

    assert_response :success
    frames = response.body.scan(/<iframe[^>]*src="([^"]+)"/).flatten.map { |src| CGI.unescapeHTML(src) }
    assert_equal 3, frames.size
    frames.each_with_index do |src, i|
      assert_equal "/play/#{@play_session.code}?sim=#{i + 1}&t=ABCDEFGH", src
    end
  end

  test "an unknown code says so" do
    get "/sim", params: { code: "ZZZZ" }

    assert_response :success
    assert_includes response.body, "No active session"
  end

  test "simulated pads may be framed by the sim page, real pads may not" do
    get "/play/#{@play_session.code}?sim=2"
    assert_includes frame_ancestors, "'self'"

    get "/play/#{@play_session.code}"
    assert_includes frame_ancestors, "'none'"
  end

  test "each simulated pad joins as its own player with its own cookie" do
    post "/play/#{@play_session.code}/join?sim=1", params: { name: "Sim 1", color: "blue" }, as: :json
    assert_response :success
    first = JSON.parse(response.body).dig("player", "id")
    assert cookies["yui_player_sim1"].present?
    assert cookies["yui_player"].blank?

    post "/play/#{@play_session.code}/join?sim=2", params: { name: "Sim 2", color: "red" }, as: :json
    assert_response :success
    second = JSON.parse(response.body).dig("player", "id")
    assert_not_equal first, second

    get "/play/#{@play_session.code}/info?sim=1"
    assert_equal first, JSON.parse(response.body).dig("player", "id")
    get "/play/#{@play_session.code}/info"
    assert_nil JSON.parse(response.body)["player"]
  end

  test "sim is ignored outside development and test" do
    with_env("production") do
      assert_not PadSim.enabled?
      assert_nil PadSim.slot("2")
      assert_equal PlayController::PLAYER_COOKIE, PadSim.cookie_name("2")
    end
    assert_equal :yui_player_sim2, PadSim.cookie_name("2")
    assert_equal PlayController::PLAYER_COOKIE, PadSim.cookie_name("5")
  end

  test "the sim controller refuses to run in production" do
    with_env("production") do
      get "/sim"
      assert_response :not_found
    end
  end

  private
    def frame_ancestors
      response.headers["Content-Security-Policy"].to_s.split(";").map(&:strip).find { |d| d.start_with?("frame-ancestors") }.to_s
    end

    def with_env(name)
      previous = Rails.env
      Rails.env = name
      yield
    ensure
      Rails.env = previous
    end
end
