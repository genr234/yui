class AddActivityClearMarkersToAccounts < ActiveRecord::Migration[8.1]
  def change
    add_column :accounts, :commands_cleared_at, :datetime
    add_column :accounts, :operations_cleared_at, :datetime
  end
end
