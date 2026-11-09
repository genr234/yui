# Save-file backups (plan 08 item 5) are `other` assets keyed per account by
# "<app id>/<runtime id>". The unique index makes replace-by-key race-safe.
class AddSaveKeyToAssets < ActiveRecord::Migration[8.1]
  def change
    add_column :assets, :save_key, :string
    add_index :assets, [ :account_id, :save_key ], unique: true, where: "save_key IS NOT NULL",
      name: "index_assets_on_account_id_and_save_key"
  end
end
