class CreatePasskeyCredentials < ActiveRecord::Migration[8.1]
  def change
    create_table :passkey_credentials do |t|
      t.references :user, null: false, foreign_key: true
      t.string :webauthn_id, null: false
      t.text :public_key, null: false
      t.integer :sign_count, default: 0, null: false
      t.string :nickname
      t.datetime :last_used_at

      t.timestamps
    end

    add_index :passkey_credentials, :webauthn_id, unique: true
  end
end
