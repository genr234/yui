class CreateAccountStateRecords < ActiveRecord::Migration[8.1]
  def change
    create_table :account_state_records do |t|
      t.references :account, null: false, foreign_key: true
      t.string :collection, null: false
      t.string :record_id, null: false
      t.json :value
      t.boolean :deleted, null: false, default: false
      t.integer :server_seq, null: false

      t.timestamps
    end

    add_index :account_state_records, [ :account_id, :collection, :record_id ], unique: true, name: "idx_account_state_identity"
  end
end
