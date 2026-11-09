# Opaque save blobs an app keeps per account (plan 11, `ctx.saves`). The bytes live in
# storage/app_saves/<account id>/<id>; `scope` says whether the account is the kiosk's
# own or a player's.
class CreateAppSaves < ActiveRecord::Migration[8.1]
  def change
    create_table :app_saves do |t|
      t.references :account, null: false, foreign_key: true
      t.string :app_id, null: false
      t.string :key, null: false
      t.string :scope, null: false
      t.string :sha256, null: false
      t.bigint :byte_size, null: false, default: 0
      t.datetime :saved_at, null: false
      t.timestamps
    end
    add_index :app_saves, [ :account_id, :app_id, :scope, :key ], unique: true, name: "index_app_saves_uniqueness"
  end
end
