class Asset < ApplicationRecord
  # Generic kinds only: console- or format-specific details live in `metadata`,
  # filled by AssetAnalyzers (e.g. metadata.platform = "wii").
  KINDS = %w[game runtime media other].freeze
  STATUSES = %w[uploading processing ready failed].freeze
  MAX_FILENAME_BYTES = 200
  # Uploads that stopped receiving data this long ago are deleted by CleanupStaleUploadsJob.
  STALE_UPLOAD_AGE = 7.days

  belongs_to :account
  # Set for games imported from a linked account (plan 16); those are re-synced, not edited.
  belongs_to :linked_account, optional: true
  has_many :kiosk_assets, dependent: :delete_all

  before_validation :normalize_filename

  validates :kind, inclusion: { in: KINDS }
  validates :status, inclusion: { in: STATUSES }
  validates :title, :filename, presence: true
  validates :title, length: { maximum: 200 }
  validates :byte_size, numericality: { only_integer: true, greater_than_or_equal_to: 0 }
  validates :sha256, format: { with: /\A\h{64}\z/ }, allow_nil: true

  validates :save_key, uniqueness: { scope: :account_id }, allow_nil: true
  validates :kind, inclusion: { in: %w[other], message: "must be other for save backups" }, if: :save_key?

  scope :ready, -> { where(status: "ready") }
  # Save-file backups (kind "other", keyed by save_key) are fetched through the kiosk saves
  # API, not the asset manifest, so kiosks do not download every other kiosk's saves.
  scope :library, -> { where(save_key: nil) }
  scope :save_backups, -> { where.not(save_key: nil) }
  scope :stale_uploads, -> { where(status: "uploading").where(updated_at: ...STALE_UPLOAD_AGE.ago) }

  after_destroy_commit :remove_files
  after_destroy_commit :request_kiosk_reconcile!, if: -> { status == "ready" }

  # Turns an arbitrary client-supplied name into a safe single path segment.
  def self.sanitize_filename(name)
    text = name.to_s.dup.force_encoding(Encoding::UTF_8).scrub("_").unicode_normalize(:nfc)
    base = File.basename(text.tr("\\", "/"))
    base = base.gsub(/[^\p{Alnum}._\- ()\[\]]/, "_").squeeze("_").strip
    base = base.sub(/\A[.\s]+/, "")
    base = base.byteslice(0, MAX_FILENAME_BYTES).scrub("") if base.bytesize > MAX_FILENAME_BYTES
    base.presence || "upload.bin"
  end

  # Title used when the uploader didn't give one: the filename without its extension.
  def self.default_title(filename)
    File.basename(filename.to_s, ".*").tr("_", " ").squeeze(" ").strip.presence || filename.to_s
  end

  STATUSES.each do |value|
    define_method(:"#{value}?") { status == value }
  end

  def save_backup?
    save_key.present?
  end

  def game?
    kind == "game"
  end

  def directory
    AssetStorage.assets_dir.join(account_id.to_s, id.to_s)
  end

  def file_path
    directory.join(filename)
  end

  # Start of a tus upload: an empty file at the final path that PATCHes append to.
  def create_empty_file!
    FileUtils.mkdir_p(directory)
    FileUtils.touch(file_path)
  end

  # Bytes on disk; for an upload in progress this is the tus offset.
  def stored_bytes
    File.size?(file_path).to_i
  end

  def game_id
    metadata["game_id"] if game?
  end

  def cover_path
    GameCover.path_for(game_id) if game_id.present?
  end

  def cover?
    cover_path&.file? || false
  end

  def imported?
    linked_account_id.present?
  end

  # Site-relative path of the cover to show in the control room, or nil.
  def cover_src
    return Rails.application.routes.url_helpers.cover_path(game_id) if cover?

    metadata["cover_url"].presence if imported? && metadata["cover_url"].to_s.start_with?("/")
  end

  # Queues an `assets.reconcile` command for every kiosk on the account.
  def request_kiosk_reconcile!
    Asset.request_reconcile_for!(account)
  end

  def self.request_reconcile_for!(account)
    account.kiosks.find_each do |kiosk|
      kiosk.kiosk_commands.create!(command_type: "assets.reconcile", payload: {})
    end
  end

  # Wire format for the kiosk API (contracts §1). `base_url` makes cover_url absolute
  # for whoever is asking, so it is right behind any host name.
  def as_kiosk_json(base_url: nil)
    {
      id: id.to_s,
      kind: kind,
      title: title,
      filename: filename,
      byte_size: byte_size,
      sha256: sha256,
      metadata: kiosk_metadata(base_url),
      updated_at: updated_at.utc.iso8601
    }
  end

  private

  def kiosk_metadata(base_url)
    data = metadata.to_h
    return data unless game?

    cover_url = base_url && cover? ? GameCover.url_for(game_id, base_url) : data["cover_url"]
    # Imported covers are stored as site-relative paths; kiosks need them absolute.
    cover_url = "#{base_url.to_s.chomp("/")}#{cover_url}" if base_url && cover_url.to_s.start_with?("/")
    data.merge("cover_url" => cover_url)
  end

  def normalize_filename
    self.filename = self.class.sanitize_filename(filename) if filename.present?
  end

  def remove_files
    FileUtils.rm_rf(directory) if id.present? && account_id.present?
    LinkedCover.remove_for(id) if imported?
  end
end
