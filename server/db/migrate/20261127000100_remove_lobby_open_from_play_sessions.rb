# Every session is joinable with its code alone, so the lobby toggle is gone.
class RemoveLobbyOpenFromPlaySessions < ActiveRecord::Migration[8.1]
  def change
    remove_column :play_sessions, :lobby_open, :boolean, null: false, default: false
  end
end
