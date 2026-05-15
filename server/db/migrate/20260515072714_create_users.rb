class CreateUsers < ActiveRecord::Migration[8.1]
  def change
    create_table :users do |t|
      t.string :name, null: false
      t.string :webauthn_id, null: false

      t.timestamps
    end

    add_index :users, :webauthn_id, unique: true
  end
end
