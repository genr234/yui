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

  after_create_commit :deliver_over_websocket!

  validates :command_type, inclusion: { in: ALLOWED_TYPES }
  validates :status, inclusion: { in: STATUSES }

  scope :deliverable, -> { where(status: %w[pending sent]).order(:created_at) }

  def mark_sent!
    update!(status: "sent", sent_at: Time.current) if pending?
  end

  def pending?
    status == "pending"
  end

  def deliver_over_websocket!
    mark_sent!
    KioskCommandsChannel.broadcast_to(kiosk, websocket_payload)
  end

  def websocket_payload
    {
      id: id.to_s,
      command_type: command_type,
      payload: payload,
      status: status
    }
  end
end
