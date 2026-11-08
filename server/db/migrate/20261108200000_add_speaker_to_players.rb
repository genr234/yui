class AddSpeakerToPlayers < ActiveRecord::Migration[8.1]
  def up
    add_column :players, :speaker, :boolean, null: false, default: false
    # Sessions already running keep the default: the host plays the sound.
    execute "UPDATE players SET speaker = host WHERE kicked_at IS NULL"
  end

  def down
    remove_column :players, :speaker
  end
end
