# Active Record encryption keys (used for linked-account credentials).
#
# Production reads three secrets from the environment, falling back to
# `active_record_encryption` in the Rails credentials when they aren't set:
#
#   ACTIVE_RECORD_ENCRYPTION_PRIMARY_KEY
#   ACTIVE_RECORD_ENCRYPTION_DETERMINISTIC_KEY
#   ACTIVE_RECORD_ENCRYPTION_KEY_DERIVATION_SALT
#
# Generate them once with `bin/rails db:encryption:init` and keep them: losing them
# makes saved credentials unreadable (owners would have to re-enter them).
# Development and test use fixed throwaway keys that protect nothing real.
encryption = Rails.application.config.active_record.encryption

if Rails.env.local?
  encryption.primary_key = ENV.fetch("ACTIVE_RECORD_ENCRYPTION_PRIMARY_KEY", "yui-dev-primary-key-not-secret")
  encryption.deterministic_key = ENV.fetch("ACTIVE_RECORD_ENCRYPTION_DETERMINISTIC_KEY", "yui-dev-deterministic-key-not-secret")
  encryption.key_derivation_salt = ENV.fetch("ACTIVE_RECORD_ENCRYPTION_KEY_DERIVATION_SALT", "yui-dev-salt-not-secret")
else
  encryption.primary_key = ENV["ACTIVE_RECORD_ENCRYPTION_PRIMARY_KEY"] if ENV["ACTIVE_RECORD_ENCRYPTION_PRIMARY_KEY"].present?
  encryption.deterministic_key = ENV["ACTIVE_RECORD_ENCRYPTION_DETERMINISTIC_KEY"] if ENV["ACTIVE_RECORD_ENCRYPTION_DETERMINISTIC_KEY"].present?
  encryption.key_derivation_salt = ENV["ACTIVE_RECORD_ENCRYPTION_KEY_DERIVATION_SALT"] if ENV["ACTIVE_RECORD_ENCRYPTION_KEY_DERIVATION_SALT"].present?
end
