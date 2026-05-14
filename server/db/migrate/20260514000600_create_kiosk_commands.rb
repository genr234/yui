class CreateKioskCommands < ActiveRecord::Migration[8.1]
  def change
    create_table :kiosk_commands do |t|
      t.references :kiosk, null: false, foreign_key: true
      t.string :command_type, null: false
      t.json :payload, null: false, default: {}
      t.string :status, null: false, default: "pending"
      t.json :result
      t.text :error
      t.datetime :sent_at
      t.datetime :completed_at

      t.timestamps
    end

    add_index :kiosk_commands, [ :kiosk_id, :status ]
  end
end
