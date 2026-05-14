require "test_helper"

class PairingCodeTest < ActiveSupport::TestCase
  test "claim returns the account once before expiry" do
    account = Account.create!(name: "Lobby")
    pairing_code, code = PairingCode.create_for!(account)

    assert_equal account, PairingCode.claim!(code)
    assert pairing_code.reload.used_at
    assert_raises(ActiveRecord::RecordNotFound) { PairingCode.claim!(code) }
  end
end
