# One thing the owner chose to bring in from a linked account: a whole platform, a
# collection, or a single game. Nothing is downloaded that isn't covered by an import.
class LinkedImport < ApplicationRecord
  KINDS = %w[platform collection rom].freeze

  belongs_to :linked_account

  validates :kind, inclusion: { in: KINDS }
  validates :remote_id, presence: true, uniqueness: { scope: [ :linked_account_id, :kind ] }
  validates :label, presence: true
end
