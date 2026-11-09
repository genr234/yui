# A player allowed one game (app id) to keep a profile in their account. Only
# the player's phone creates these (plan 10, P3).
class ProfileGrant < ApplicationRecord
  APP_ID_FORMAT = /\A[a-z0-9][a-z0-9._-]{0,127}\z/i

  belongs_to :account

  validates :app_id, presence: true, format: { with: APP_ID_FORMAT }, uniqueness: { scope: :account_id }
end
