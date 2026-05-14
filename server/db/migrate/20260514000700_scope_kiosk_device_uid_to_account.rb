class ScopeKioskDeviceUidToAccount < ActiveRecord::Migration[8.1]
  def change
    remove_index :kiosks, :device_uid
    add_index :kiosks, [ :account_id, :device_uid ], unique: true
  end
end
