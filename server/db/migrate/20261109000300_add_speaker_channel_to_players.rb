# Stereo speaker pair (plan 08 item 4): two speaker phones carry "left" and
# "right". NULL is the mono speaker (the whole mix), as before.
class AddSpeakerChannelToPlayers < ActiveRecord::Migration[8.1]
  def change
    add_column :players, :speaker_channel, :string
  end
end
