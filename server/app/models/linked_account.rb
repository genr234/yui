# An outside service (RomM first) that holds the owner's own files. Credentials are Active
# Record encrypted, never logged, never sent to kiosks and never shown again after saving.
# What gets brought in is chosen explicitly through LinkedImports and fetched by
# LinkedAccountSyncJob into ordinary `game` assets (plan 16).
class LinkedAccount < ApplicationRecord
  STATUSES = %w[ok error syncing].freeze
  # A sync that claims to be running for longer than this is assumed dead.
  SYNC_STALE_AFTER = 2.hours

  belongs_to :account
  has_many :linked_imports, dependent: :destroy
  has_many :assets, dependent: :nullify

  encrypts :credentials

  before_validation :normalize

  validates :provider, inclusion: { in: ->(_) { LinkedProviders.names } }
  validates :label, presence: true, length: { maximum: 100 }
  validates :status, inclusion: { in: STATUSES }
  validate :base_url_is_acceptable
  validate :credentials_are_present

  scope :syncable, -> { where.not(status: "syncing").or(where(syncing_since: ...SYNC_STALE_AFTER.ago)) }

  # Never serialised with secrets, however it is rendered.
  def serializable_hash(options = nil)
    super((options || {}).merge(except: %w[credentials] + Array(options&.dig(:except))))
  end

  def provider_label
    LinkedProviders.label_for(provider)
  end

  def credential_data
    JSON.parse(credentials.presence || "{}")
  rescue JSON::ParserError
    {}
  end

  # Accepts the form fields. Blank secrets keep what is already saved, so editing the
  # URL doesn't force re-entering the password.
  def assign_credentials(auth:, token: nil, username: nil, password: nil)
    current = credential_data
    data = if auth.to_s == "token"
      { "auth" => "token", "token" => token.presence || (current["auth"] == "token" ? current["token"] : nil) }
    else
      same_user = current["auth"] == "password" && (username.blank? || username == current["username"])
      { "auth" => "password", "username" => username.presence || current["username"],
        "password" => password.presence || (same_user ? current["password"] : nil) }
    end
    self.credentials = data.compact.to_json
  end

  # What the control room may show of the saved credentials: the user, never the secret.
  def credential_summary
    data = credential_data
    data["auth"] == "token" ? "API token •••" : "#{data["username"]} •••"
  end

  def syncing?
    status == "syncing" && syncing_since.present? && syncing_since > SYNC_STALE_AFTER.ago
  end

  # Claims the sync slot; false when another run holds it.
  def begin_sync!
    claimed = self.class.syncable.where(id: id).update_all(status: "syncing", syncing_since: Time.current, updated_at: Time.current)
    reload if claimed == 1
    claimed == 1
  end

  def finish_sync!(error: nil, status: nil)
    update!(status: status || (error ? "error" : "ok"), last_error: error, syncing_since: nil, last_synced_at: Time.current)
  end

  def provider_client
    LinkedProviders.for(self)
  end

  # "142 games from 3 platforms, 1 collection. 18.4 GB."
  def summary
    games = assets.listed
    fetched = assets.ready
    counts = linked_imports.group(:kind).count
    parts = { "platform" => "platform", "collection" => "collection", "rom" => "single game" }.filter_map do |kind, noun|
      ApplicationController.helpers.pluralize(counts[kind], noun) if counts[kind].to_i.positive?
    end
    helpers = ApplicationController.helpers
    text = "#{helpers.pluralize(games.count, "game")}"
    text += " from #{parts.to_sentence}" if parts.any?
    "#{text}. #{fetched.count} downloaded (#{helpers.number_to_human_size(fetched.sum(:byte_size))}); the rest download when a kiosk asks."
  end

  private

  def normalize
    self.provider = provider.to_s.strip.downcase
    self.base_url = base_url.to_s.strip.chomp("/")
    self.label = label.to_s.strip
  end

  def base_url_is_acceptable
    problem = LinkedProviders::HttpClient.url_problem(base_url, allow_local_network: allow_local_network)
    errors.add(:base_url, problem) if problem
  end

  def credentials_are_present
    data = credential_data
    secret = data["auth"] == "token" ? data["token"] : (data["username"].present? && data["password"])
    errors.add(:base, "Enter an API token, or a username and password") if secret.blank?
  end
end
