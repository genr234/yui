class KioskCommand < ApplicationRecord
  ALLOWED_TYPES = %w[
    apps.sources.add
    apps.sources.remove
    apps.sources.refresh
    apps.install
    apps.uninstall
    plugins.sources.add
    plugins.sources.remove
    plugins.sources.refresh
    plugins.install
    plugins.uninstall
    plugins.enable
    plugins.disable
    plugins.permissions.update
    plugins.settings.update
  ].freeze

  STATUSES = %w[pending sent succeeded failed].freeze

  belongs_to :kiosk

  validates :command_type, inclusion: { in: ALLOWED_TYPES }
  validates :status, inclusion: { in: STATUSES }

  scope :deliverable, -> { where(status: %w[pending sent]).order(:created_at) }

  def mark_sent!
    update!(status: "sent", sent_at: Time.current) if pending?
  end

  def pending?
    status == "pending"
  end
end
