class PasskeyCredential < ApplicationRecord
  belongs_to :user

  validates :webauthn_id, :public_key, :sign_count, presence: true
  validates :webauthn_id, uniqueness: true
end
