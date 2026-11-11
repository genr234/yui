# Sends one command to a kiosk and prints its answer as JSON, for debugging a
# kiosk without being there. On the Nest box:
#
#   /srv/yui/bin/yui-rails runner script/kiosk_debug.rb <kiosk id or name> <command> ['{"json":"params"}'] [timeout seconds]
#
#   ... 1 debug.diagnostics
#   ... 1 debug.logs '{"source":"native","lines":300}'
#   ... 1 debug.screen          (result.image is a data:image/jpeg;base64 URI)
#   ... 1 debug.exec '{"script":"Get-NetIPConfiguration","timeout":60}' 70
#         (PowerShell by default, or "shell":"cmd"; only from this console)
#
# Commands are the kiosk's remote ones (KioskCommand::ALLOWED_TYPES and
# CONSOLE_ONLY_TYPES).
require "json"

target, type, params, timeout = ARGV
abort "usage: kiosk_debug.rb <kiosk> <command> [json] [timeout]" if target.blank? || type.blank?

kiosk = target.match?(/\A\d+\z/) ? Kiosk.find(target) : Kiosk.find_by!(name: target)
command = kiosk.kiosk_commands.create!(command_type: type, payload: params.present? ? JSON.parse(params) : {})
deadline = Time.current + (timeout.presence || 30).to_i
until %w[succeeded failed].include?(command.reload.status) || Time.current > deadline
  sleep 0.25
end
puts JSON.pretty_generate(
  kiosk: kiosk.name, online: kiosk.online?, command: type, status: command.status,
  error: command.error, result: command.result
)
