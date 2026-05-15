class ScopeKioskOperationClientSequenceToAccount < ActiveRecord::Migration[8.1]
  def change
    remove_index :kiosk_operations, name: "index_kiosk_operations_on_client_id_and_client_seq"
    add_index :kiosk_operations,
      [ :account_id, :client_id, :client_seq ],
      unique: true,
      name: "idx_kiosk_operations_account_client_seq"
  end
end
