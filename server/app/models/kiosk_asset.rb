# Per-kiosk sync progress for one asset, reported by the kiosk (contracts §1).
class KioskAsset < ApplicationRecord
  STATES = %w[queued downloading paused verifying ready failed].freeze
  # States in which a kiosk is actively working on the asset.
  ACTIVE_STATES = %w[queued downloading verifying].freeze

  belongs_to :kiosk
  belongs_to :asset

  validates :state, inclusion: { in: STATES }
  validates :bytes_done, numericality: { only_integer: true, greater_than_or_equal_to: 0 }
  validates :asset_id, uniqueness: { scope: :kiosk_id }

  def active?
    ACTIVE_STATES.include?(state)
  end
end
