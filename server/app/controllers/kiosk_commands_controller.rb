class KioskCommandsController < ApplicationController
  ID_PAYLOAD_COMMANDS = %w[
    apps.sources.remove
    apps.sources.refresh
    apps.uninstall
    plugins.sources.remove
    plugins.sources.refresh
    plugins.uninstall
    plugins.enable
    plugins.disable
  ].freeze

  CATALOG_PAYLOAD_COMMANDS = %w[
    apps.install
    plugins.install
  ].freeze

  def create
    account = Account.find(params[:account_id])
    kiosk = account.kiosks.find(params[:kiosk_id])
    payload = parse_payload(params[:command_type], params[:payload])

    kiosk.kiosk_commands.create!(
      command_type: params[:command_type],
      payload: payload
    )
    redirect_to account, notice: "Command queued."
  rescue JSON::ParserError => error
    redirect_to account, alert: "Invalid JSON payload: #{error.message}"
  end

  private

  def parse_payload(command_type, value)
    return {} if value.blank?

    JSON.parse(value)
  rescue JSON::ParserError
    shorthand_payload(command_type, value) || raise
  end

  def shorthand_payload(command_type, value)
    text = value.to_s.strip
    text = text.delete_prefix("{").delete_suffix("}").strip
    text = text.delete_prefix("\"").delete_suffix("\"").strip
    return if text.blank? || text.include?(":")

    return { id: text } if ID_PAYLOAD_COMMANDS.include?(command_type)

    { catalogId: text } if CATALOG_PAYLOAD_COMMANDS.include?(command_type)
  end
end
