class AccountStateRecord < ApplicationRecord
  belongs_to :account

  validates :collection, :record_id, :server_seq, presence: true

  def self.upsert_state!(account:, collection:, record_id:, value:, server_seq:, deleted: false)
    record = account.account_state_records.find_or_initialize_by(
      collection: collection,
      record_id: record_id
    )
    return record if record.persisted? && record.server_seq > server_seq.to_i

    record.value = value
    record.deleted = deleted
    record.server_seq = server_seq
    record.save!
    record
  end
end
