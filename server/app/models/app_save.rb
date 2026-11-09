# One save blob an app keeps for an account (plan 11, `ctx.saves`). The app picks the
# key and the bytes; nothing here knows what they mean. `scope` separates the kiosk
# account's own blobs from a player's, since the same account can hold both.
class AppSave < ApplicationRecord
  SCOPES = %w[kiosk player].freeze
  KEY_FORMAT = /\A[a-z0-9][a-z0-9._-]{0,79}\z/
  MAX_BYTES = ENV.fetch("APP_SAVE_MAX_BYTES", 8.megabytes).to_i
  MAX_KEYS = 64

  belongs_to :account

  validates :app_id, presence: true, format: { with: ProfileGrant::APP_ID_FORMAT }
  validates :key, format: { with: KEY_FORMAT }, uniqueness: { scope: %i[account_id app_id scope] }
  validates :scope, inclusion: { in: SCOPES }
  validates :sha256, format: { with: /\A\h{64}\z/ }
  validates :byte_size, numericality: { only_integer: true, greater_than_or_equal_to: 0, less_than_or_equal_to: MAX_BYTES }

  after_destroy_commit :remove_file

  def self.valid_key?(key)
    key.is_a?(String) && KEY_FORMAT.match?(key)
  end

  def directory
    AssetStorage.app_saves_dir.join(account_id.to_s)
  end

  def file_path
    directory.join(id.to_s)
  end

  # Moves `source` (a path) into place with a rename, so a reader sees the old or
  # the new blob, never half of one.
  def install_file(source)
    FileUtils.mkdir_p(directory)
    staged = directory.join(".incoming-#{SecureRandom.hex(4)}")
    FileUtils.cp(source, staged)
    File.rename(staged, file_path)
  ensure
    FileUtils.rm_f(staged) if staged
  end

  def summary_json
    { key: key, size: byte_size, sha256: sha256, updated_at: saved_at.utc.iso8601 }
  end

  private

  def remove_file
    FileUtils.rm_f(file_path)
  end
end
