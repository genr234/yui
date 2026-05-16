class KioskOperation < ApplicationRecord
  ACTIONS = %w[put delete replace_collection].freeze
  COLLECTIONS = %w[
    storage
    app-sources
    app-catalog
    installed-apps
    app-storage
    plugin-sources
    plugin-catalog
    installed-plugins
    plugin-state
    plugin-settings
    plugin-secrets
    plugin-storage
  ].freeze
  MAX_PAYLOAD_BYTES = 256.kilobytes
  MAX_REPLACE_COLLECTION_PAYLOAD_BYTES = 2.megabytes

  belongs_to :account
  belongs_to :kiosk, optional: true

  validates :client_id, :client_seq, :server_seq, :collection, :action, presence: true
  validates :action, inclusion: { in: ACTIONS }
  validates :collection, inclusion: { in: COLLECTIONS }
  validates :client_id, :record_id, length: { maximum: 200 }, allow_blank: true
  validates :client_seq, numericality: { only_integer: true, greater_than: 0 }
  validates :client_seq, uniqueness: { scope: [ :account_id, :client_id ] }
  validates :server_seq, uniqueness: { scope: :account_id }
  validate :record_id_matches_action
  validate :payload_size_is_bounded

  after_commit :apply_to_state, on: :create

  def self.accept!(account:, kiosk:, attributes:)
    account.with_lock do
      existing = account.kiosk_operations.find_by(
        client_id: attributes.fetch(:client_id),
        client_seq: attributes.fetch(:client_seq)
      )
      existing || account.kiosk_operations.create!(
        kiosk: kiosk,
        client_id: attributes.fetch(:client_id),
        client_seq: attributes.fetch(:client_seq),
        collection: attributes.fetch(:collection),
        record_id: attributes[:record_id],
        action: attributes.fetch(:action),
        payload: attributes[:payload],
        occurred_at: attributes[:occurred_at],
        server_seq: account.kiosk_operations.maximum(:server_seq).to_i + 1
      )
    end
  end

  private

  def record_id_matches_action
    if action == "replace_collection"
      errors.add(:record_id, "must be blank for replace_collection") if record_id.present?
    elsif record_id.blank?
      errors.add(:record_id, "is required")
    end
  end

  def payload_size_is_bounded
    bytes = ActiveSupport::JSON.encode(payload).bytesize
    limit = action == "replace_collection" ? MAX_REPLACE_COLLECTION_PAYLOAD_BYTES : MAX_PAYLOAD_BYTES
    errors.add(:payload, "is too large") if bytes > limit
  end

  def apply_to_state
    if action == "replace_collection"
      account.account_state_records.where(collection: collection).delete_all
      Array(payload).each do |doc|
        AccountStateRecord.upsert_state!(
          account: account,
          collection: collection,
          record_id: doc.fetch("id"),
          value: doc["value"],
          server_seq: server_seq
        )
      end
      return
    end

    AccountStateRecord.upsert_state!(
      account: account,
      collection: collection,
      record_id: record_id,
      value: payload,
      server_seq: server_seq,
      deleted: action == "delete"
    )
  end
end
