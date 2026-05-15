class Account < ApplicationRecord
  has_one_attached :profile_image

  has_many :kiosks, dependent: :destroy
  has_many :pairing_codes, dependent: :destroy
  has_many :kiosk_operations, dependent: :destroy
  has_many :account_state_records, dependent: :destroy

  validates :name, presence: true
  validates :profile_image_url, length: { maximum: 2048 }, allow_blank: true
  validate :profile_image_is_supported

  private

  def profile_image_is_supported
    return unless profile_image.attached?

    unless profile_image.content_type.in?(%w[image/png image/jpeg image/gif image/webp])
      errors.add(:profile_image, "must be a PNG, JPEG, GIF, or WebP image")
    end

    if profile_image.byte_size > 5.megabytes
      errors.add(:profile_image, "must be smaller than 5 MB")
    end
  end
end
