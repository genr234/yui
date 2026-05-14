class AddProfileImageUrlToAccounts < ActiveRecord::Migration[8.1]
  def change
    add_column :accounts, :profile_image_url, :string
  end
end
