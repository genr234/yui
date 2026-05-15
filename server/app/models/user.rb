class User < ApplicationRecord
  has_many :passkey_credentials, dependent: :destroy

  validates :name, presence: true
  validates :webauthn_id, presence: true, uniqueness: true

  before_validation :ensure_webauthn_id

  private

  def ensure_webauthn_id
    self.webauthn_id ||= WebAuthn.generate_user_id
  end
end
