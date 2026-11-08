# The Yui Pad service worker is a static file at /pad/sw.js but registers with
# scope "/play/", which browsers only allow when the script response carries
# `Service-Worker-Allowed: /play/`.
class PadServiceWorkerHeader
  PATH = "/pad/sw.js".freeze
  SCOPE = "/play/".freeze

  def initialize(app)
    @app = app
  end

  def call(env)
    status, headers, body = @app.call(env)
    headers["service-worker-allowed"] = SCOPE if env["PATH_INFO"] == PATH
    [ status, headers, body ]
  end
end

Rails.application.config.middleware.insert_before 0, PadServiceWorkerHeader
