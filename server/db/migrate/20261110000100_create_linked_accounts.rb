class CreateLinkedAccounts < ActiveRecord::Migration[8.1]
  def change
    create_table :linked_accounts do |t|
      t.references :account, null: false, foreign_key: true
      t.string :provider, null: false
      t.string :label, null: false
      t.string :base_url, null: false
      t.boolean :allow_local_network, null: false, default: false
      # Active Record encrypted JSON; never shown again after saving.
      t.text :credentials
      t.string :status, null: false, default: "ok"
      t.text :last_error
      t.datetime :last_synced_at
      t.datetime :syncing_since
      t.timestamps
    end

    create_table :linked_imports do |t|
      t.references :linked_account, null: false, foreign_key: true
      t.string :kind, null: false
      t.string :remote_id, null: false
      t.string :label, null: false
      t.timestamps
    end
    add_index :linked_imports, [ :linked_account_id, :kind, :remote_id ], unique: true

    # Which linked account an imported game came from (null for uploads).
    add_reference :assets, :linked_account, foreign_key: true, index: true
  end
end
