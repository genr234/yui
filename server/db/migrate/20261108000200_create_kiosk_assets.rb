class CreateKioskAssets < ActiveRecord::Migration[8.1]
  def change
    create_table :kiosk_assets do |t|
      t.references :kiosk, null: false, foreign_key: { on_delete: :cascade }, index: false
      t.references :asset, null: false, foreign_key: { on_delete: :cascade }
      t.string :state, null: false, default: "queued"
      t.bigint :bytes_done, null: false, default: 0
      t.text :error
      t.timestamps
    end

    add_index :kiosk_assets, [ :kiosk_id, :asset_id ], unique: true
  end
end
