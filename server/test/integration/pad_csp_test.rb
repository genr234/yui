require "test_helper"

class PadCspTest < ActionDispatch::IntegrationTest
  include PlaySessionTestHelper

  setup do
    @play_session = start_session
  end

  test "play page CSP allows external scripts and blocks inline ones" do
    dir = Rails.root.join("tmp", "pad-csp-#{SecureRandom.hex(4)}")
    FileUtils.mkdir_p(dir)
    html = <<~HTML
      <!doctype html>
      <html>
        <head>
          <link rel="stylesheet" href="/pad/assets/index.css">
        </head>
        <body>
          <div id="app"></div>
          <script type="module" src="/pad/assets/index.js"></script>
        </body>
      </html>
    HTML
    File.write(dir.join("index.html"), html)

    previous = PlayController.pad_index
    PlayController.pad_index = dir.join("index.html")
    get "/play/#{@play_session.code}"

    assert_response :success
    csp = response.headers["Content-Security-Policy"].to_s
    script_src = directive(csp, "script-src")
    assert_includes script_src, "'self'"
    assert_match(/'nonce-[^']+'/, script_src)
    assert_not_includes script_src, "unsafe-inline"
    assert_includes directive(csp, "style-src-attr"), "'unsafe-inline'"
    assert_no_inline_scripts(response.body)
  ensure
    PlayController.pad_index = previous if defined?(previous) && previous
    FileUtils.rm_rf(dir) if dir
  end

  test "pad shell and vite build contain no inline scripts" do
    source = Rails.root.join("../pad/index.html").read
    assert_no_inline_scripts(source)

    built = Rails.root.join("public/pad/index.html")
    assert built.file?, "run just build-pad before this test"
    built_html = built.read
    assert_no_inline_scripts(built_html)
    assert_includes built_html, 'src="/pad/'
  end

  private
    def directive(csp, name)
      csp.split(";").map(&:strip).find { |part| part.start_with?("#{name} ") }.to_s
    end

    def assert_no_inline_scripts(html)
      tags = html.scan(/<script\b([^>]*)>(.*?)<\/script>/mi)
      assert tags.any?, "expected at least one external script"
      tags.each do |attrs, body|
        assert_match(/\ssrc\s*=\s*["'][^"']+["']/i, " #{attrs}")
        assert_not_includes attrs, "javascript:"
        assert_predicate body.strip, :blank?, "inline script body: #{body.inspect}"
      end
    end
end
