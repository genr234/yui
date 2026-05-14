require "test_helper"

class KioskPairingTest < ActionDispatch::IntegrationTest
  test "same physical kiosk can pair to multiple accounts" do
    first = Account.create!(name: "First")
    second = Account.create!(name: "Second")
    _first_pairing, first_code = PairingCode.create_for!(first)
    _second_pairing, second_code = PairingCode.create_for!(second)

    post "/api/kiosk/pair", params: {
      code: first_code,
      device_uid: "same-device",
      name: "Front kiosk"
    }, as: :json
    assert_response :success

    post "/api/kiosk/pair", params: {
      code: second_code,
      device_uid: "same-device",
      name: "Front kiosk"
    }, as: :json
    assert_response :success

    assert_equal 2, Kiosk.where(device_uid: "same-device").count
    assert_equal [ first.id, second.id ].sort, Kiosk.where(device_uid: "same-device").pluck(:account_id).sort
  end
end
