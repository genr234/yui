class KioskOperation < ApplicationRecord
  ACTIONS = %w[put delete replace_collection].freeze

  belongs_to :account
  belongs_to :kiosk, optional: true

  validates :client_id, :client_seq, :server_seq, :collection, :action, presence: true
  validates :action, inclusion: { in: ACTIONS }
  validates :client_seq, uniqueness: { scope: :client_id }
  validates :server_seq, uniqueness: { scope: :account_id }

  before_validation :assign_server_seq, on: :create
  after_commit :apply_to_state, on: :create

  def self.accept!(account:, kiosk:, attributes:)
    create!(
      account: account,
      kiosk: kiosk,
      client_id: attributes.fetch(:client_id),
      client_seq: attributes.fetch(:client_seq),
      collection: attributes.fetch(:collection),
      record_id: attributes[:record_id],
      action: attributes.fetch(:action),
      payload: attributes[:payload],
      occurred_at: attributes[:occurred_at]
    )
  rescue ActiveRecord::RecordNotUnique, ActiveRecord::RecordInvalid
    existing = find_by!(client_id: attributes.fetch(:client_id), client_seq: attributes.fetch(:client_seq))
    existing
  end

  private

  def assign_server_seq
    return if server_seq.present?

    self.server_seq = (account.kiosk_operations.maximum(:server_seq) || 0) + 1
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
