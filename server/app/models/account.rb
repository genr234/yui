class Account < ApplicationRecord
  has_many :kiosks, dependent: :destroy
  has_many :pairing_codes, dependent: :destroy
  has_many :kiosk_operations, dependent: :destroy
  has_many :account_state_records, dependent: :destroy

  validates :name, presence: true
  validates :profile_image_url, length: { maximum: 2048 }, allow_blank: true
end
