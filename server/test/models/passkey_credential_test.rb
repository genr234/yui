require "test_helper"

class PasskeyCredentialTest < ActiveSupport::TestCase
  test "requires credential material" do
    credential = PasskeyCredential.new(user: User.new(name: "Admin"))

    assert_not credential.valid?
    assert_includes credential.errors[:webauthn_id], "can't be blank"
    assert_includes credential.errors[:public_key], "can't be blank"
  end
end
