# An emoji or a small selfie data URL, kept only while the session runs
# (PlaySession#end! clears it).
class AddAvatarToPlayers < ActiveRecord::Migration[8.1]
  def change
    add_column :players, :avatar, :text
  end
end
