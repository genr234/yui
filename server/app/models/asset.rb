class Asset < ApplicationRecord
  KINDS = %w[wii-game gamecube-game runtime media other].freeze
  GAME_KINDS = %w[wii-game gamecube-game].freeze
  STATUSES = %w[uploading processing ready failed].freeze
  MAX_FILENAME_BYTES = 200
  # Uploads that stopped receiving data this long ago are deleted by CleanupStaleUploadsJob.
  STALE_UPLOAD_AGE = 7.days

  belongs_to :account
  has_many :kiosk_assets, dependent: :delete_all

  before_validation :normalize_filename

  validates :kind, inclusion: { in: KINDS }
  validates :status, inclusion: { in: STATUSES }
  validates :title, :filename, presence: true
  validates :title, length: { maximum: 200 }
  validates :byte_size, numericality: { only_integer: true, greater_than_or_equal_to: 0 }
  validates :sha256, format: { with: /\A\h{64}\z/ }, allow_nil: true

  scope :ready, -> { where(status: "ready") }
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

  def game?
    GAME_KINDS.include?(kind)
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

    data.merge("cover_url" => (base_url && cover? ? GameCover.url_for(game_id, base_url) : data["cover_url"]))
  end

  def normalize_filename
    self.filename = self.class.sanitize_filename(filename) if filename.present?
  end

  def remove_files
    FileUtils.rm_rf(directory) if id.present? && account_id.present?
  end
end
