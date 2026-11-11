# Frees the slot and roles of players whose phone went silent without the
# cable saying so (see PlaySession#release_stale_players!).
class ReleaseStalePlayersJob < ApplicationJob
  queue_as :default

  def perform
    PlaySession.active.find_each(&:release_stale_players!)
  end
end
