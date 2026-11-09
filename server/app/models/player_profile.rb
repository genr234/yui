# One game's data for one account (plan 10, P4). Writes name the version they
# read; a stale version is a conflict, so two kiosks can't silently overwrite
# each other.
class PlayerProfile < ApplicationRecord
  MAX_BYTES = 64.kilobytes

  class VersionConflict < StandardError
    attr_reader :current

    def initialize(current)
      @current = current
      super("profile version is #{current.version}")
    end
  end

  class TooLarge < StandardError; end

  belongs_to :account

  validates :app_id, presence: true, format: { with: ProfileGrant::APP_ID_FORMAT }, uniqueness: { scope: :account_id }

  # Opaque per account and app, so a game never learns who the account is and
  # two games can't match their players up.
  def self.profile_id(account, app_id)
    OpenSSL::HMAC.hexdigest("SHA256", Rails.application.key_generator.generate_key("yui-profile-id"), "#{account.id}:#{app_id}").first(24)
  end

  def self.read(account, app_id)
    find_by(account: account, app_id: app_id) || new(account: account, app_id: app_id, version: 0)
  end

  # Replaces the data when `version` is current. Returns the saved profile.
  def self.write!(account, app_id, data, version)
    json = JSON.generate(data)
    raise TooLarge if json.bytesize > MAX_BYTES

    transaction do
      profile = lock.find_by(account: account, app_id: app_id) || new(account: account, app_id: app_id, version: 0)
      raise VersionConflict.new(profile) unless profile.version == version.to_i

      profile.data = json
      profile.version += 1
      profile.save!
      profile
    end
  rescue ActiveRecord::RecordNotUnique
    raise VersionConflict.new(read(account, app_id))
  end

  def parsed_data
    data.present? ? JSON.parse(data) : nil
  end
end
