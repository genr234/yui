# Plan 10: phones can play as a Yui account, and games keep per-account profiles.
class CreatePlayerAccountsAndProfiles < ActiveRecord::Migration[8.1]
  def change
    # "kiosk" codes pair a kiosk; "phone" codes link a phone (never interchangeable).
    add_column :pairing_codes, :purpose, :string, null: false, default: "kiosk"

    create_table :phone_links do |t|
      t.references :account, null: false, foreign_key: true
      t.string :token_digest, null: false
      t.string :label
      t.datetime :last_used_at
      t.datetime :revoked_at
      t.timestamps
    end
    add_index :phone_links, :token_digest, unique: true

    add_reference :players, :account, foreign_key: true
    add_reference :players, :phone_link, foreign_key: true

    create_table :profile_grants do |t|
      t.references :account, null: false, foreign_key: true
      t.string :app_id, null: false
      t.timestamps
    end
    add_index :profile_grants, [ :account_id, :app_id ], unique: true

    create_table :player_profiles do |t|
      t.references :account, null: false, foreign_key: true
      t.string :app_id, null: false
      t.text :data
      t.integer :version, null: false, default: 0
      t.timestamps
    end
    add_index :player_profiles, [ :account_id, :app_id ], unique: true
  end
end
