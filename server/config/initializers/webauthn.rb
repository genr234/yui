WebAuthn.configure do |config|
  origins = ENV.fetch("WEBAUTHN_ALLOWED_ORIGINS", "").split(",").map(&:strip).reject(&:blank?)
  origins += [
    "http://localhost:3000",
    "http://127.0.0.1:3000",
    "http://[::1]:3000",
    "http://www.example.com"
  ] if Rails.env.local?

  config.allowed_origins = origins.uniq
  config.rp_name = ENV.fetch("WEBAUTHN_RP_NAME", "Yui")
  config.rp_id = ENV["WEBAUTHN_RP_ID"].presence
  config.credential_options_timeout = 120_000
end
