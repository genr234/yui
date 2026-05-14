class CreatePairingCodes < ActiveRecord::Migration[8.1]
  def change
    create_table :pairing_codes do |t|
      t.references :account, null: false, foreign_key: true
      t.string :code_digest, null: false
      t.datetime :expires_at, null: false
      t.datetime :used_at

      t.timestamps
    end

    add_index :pairing_codes, :code_digest, unique: true
  end
end
