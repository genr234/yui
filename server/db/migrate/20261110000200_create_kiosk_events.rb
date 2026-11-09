class CreateKioskEvents < ActiveRecord::Migration[8.1]
  def change
    create_table :kiosk_events do |t|
      t.references :kiosk, null: false, foreign_key: true
      t.string :kind, null: false
      t.datetime :occurred_at, null: false
      t.json :data, null: false, default: {}
      t.timestamps
    end
    add_index :kiosk_events, [ :kiosk_id, :occurred_at ]

    add_column :kiosks, :health, :json, null: false, default: {}
    add_column :kiosks, :health_at, :datetime
  end
end
