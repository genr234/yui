# Linked-account providers (plan 16). A provider is the only code that knows an outside
# service's API; everything else in Yui sees ordinary `game` assets. Each provider lives in
# `linked_providers/<name>.rb` and implements:
#
#   verify!                                  -> { version:, rom_count: }  (raises on any problem)
#   platforms                                -> [{ remote_id:, slug:, label:, count: }]
#   collections                              -> [{ remote_id:, label:, count: }]
#   catalog(page:, query:, platform:, collection:, per_page:)
#                                            -> { items: [item], total:, page:, per_page: }
#   item(remote_id)                          -> one catalog item
#   download(remote_id) { |source, filename, size| }
#                                            `source.each_chunk { |bytes| }` streams the file;
#                                            `size` may be nil when the service zips on the fly.
#   cover(remote_id)                         -> LinkedProviders::Cover or nil
#
# Catalog items are hashes: remote_id, title, platform (Yui's slug where known), region,
# filename, size, multi, cover_path.
module LinkedProviders
  class Error < StandardError; end
  # The service refused the credentials.
  class AuthError < Error; end
  # Unreachable, timed out, TLS trouble, or "that isn't the service we expected".
  class ConnectionError < Error; end
  # The address is not allowed (SSRF guard).
  class UnsafeAddressError < Error; end
  # The service answered in a way this code wasn't written for.
  class ApiChangedError < Error; end
  class NotFoundError < Error; end

  Cover = Data.define(:bytes, :content_type)

  PROVIDERS = { "romm" => "LinkedProviders::Romm" }.freeze

  def self.names
    PROVIDERS.keys
  end

  def self.for(linked_account)
    PROVIDERS.fetch(linked_account.provider).constantize.new(linked_account)
  end

  def self.label_for(provider)
    { "romm" => "RomM" }.fetch(provider, provider.to_s.humanize)
  end
end
