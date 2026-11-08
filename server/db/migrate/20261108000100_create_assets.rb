class CreateAssets < ActiveRecord::Migration[8.1]
  def change
    create_table :assets do |t|
      t.references :account, null: false, foreign_key: true
      t.string :kind, null: false
      t.string :title, null: false
      t.string :filename, null: false
      t.bigint :byte_size, null: false, default: 0
      t.string :sha256
      t.json :metadata, null: false, default: {}
      t.string :status, null: false, default: "uploading"
      t.text :error
      t.timestamps
    end

    add_index :assets, [ :account_id, :status ]
    add_index :assets, [ :status, :updated_at ]
  end
end
