require "test_helper"

class ContentSecurityPolicyTest < ActionDispatch::IntegrationTest
  test "sessionless pages get a non-empty script nonce that matches the importmap tags" do
    get setup_path

    assert_response :success
    assert_nil cookies[Rails.application.config.session_options[:key]].presence,
      "expected /setup to be served without an existing session"

    nonce = script_src_nonce
    assert nonce.present?, "expected a non-empty script-src nonce, got: #{csp_header.inspect}"

    assert_select "meta[name=csp-nonce][content=?]", nonce
    assert_select "script[type=importmap]" do |scripts|
      scripts.each { |script| assert_equal nonce, script["nonce"] }
    end
    assert_select "script[type=module]" do |scripts|
      scripts.each { |script| assert_equal nonce, script["nonce"] }
    end
  end

  test "each request gets a fresh nonce" do
    get setup_path
    first = script_src_nonce

    get setup_path
    second = script_src_nonce

    assert first.present?
    assert second.present?
    assert_not_equal first, second
  end

  private
    def csp_header
      response.headers["Content-Security-Policy"].to_s
    end

    def script_src_nonce
      script_src = csp_header.split(";").map(&:strip).find { |directive| directive.start_with?("script-src ") }
      assert script_src, "expected a script-src directive, got: #{csp_header.inspect}"

      script_src[/'nonce-([^']*)'/, 1]
    end
end
