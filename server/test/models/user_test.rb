require "test_helper"

class UserTest < ActiveSupport::TestCase
  test "generates a webauthn id" do
    user = User.create!(name: "Admin")

    assert user.webauthn_id.present?
  end
end
