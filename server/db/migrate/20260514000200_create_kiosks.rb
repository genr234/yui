class CreateKiosks < ActiveRecord::Migration[8.1]
  def change
    create_table :kiosks do |t|
      t.references :account, null: false, foreign_key: true
      t.string :name, null: false
      t.string :device_uid, null: false
      t.string :device_token_digest, null: false
      t.datetime :last_seen_at
      t.datetime :connected_at

      t.timestamps
    end

    add_index :kiosks, :device_uid, unique: true
    add_index :kiosks, :device_token_digest, unique: true
  end
end
