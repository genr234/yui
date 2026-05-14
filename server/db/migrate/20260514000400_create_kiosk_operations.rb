class CreateKioskOperations < ActiveRecord::Migration[8.1]
  def change
    create_table :kiosk_operations do |t|
      t.references :account, null: false, foreign_key: true
      t.references :kiosk, null: true, foreign_key: true
      t.string :client_id, null: false
      t.integer :client_seq, null: false
      t.integer :server_seq, null: false
      t.string :collection, null: false
      t.string :record_id
      t.string :action, null: false
      t.json :payload
      t.datetime :occurred_at

      t.timestamps
    end

    add_index :kiosk_operations, [ :client_id, :client_seq ], unique: true
    add_index :kiosk_operations, [ :account_id, :server_seq ], unique: true
    add_index :kiosk_operations, [ :account_id, :collection ]
  end
end
