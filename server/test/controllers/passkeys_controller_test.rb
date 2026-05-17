require "test_helper"

class PasskeysControllerTest < ActionDispatch::IntegrationTest
  test "requires login to manage passkeys" do
    get passkeys_path

    assert_redirected_to login_path
  end

  test "lists current user's passkeys" do
    sign_in_with_passkey

    get passkeys_path

    assert_response :success
    assert_select "h1", "Passkeys"
    assert_select ".passkey-list li", 1
  end

  test "adds another passkey while signed in" do
    user = sign_in_with_passkey

    get setup_path
    assert_response :success

    post passkey_options_path
    assert_response :success
    creation_options = JSON.parse(response.body)

    client = WebAuthn::FakeClient.new("http://www.example.com")
    credential = client.create(
      challenge: creation_options.fetch("challenge"),
      user_verified: true
    )

    assert_difference -> { user.passkey_credentials.reload.count }, 1 do
      post passkeys_path, params: {
        nickname: "Phone",
        public_key_credential: credential.to_json
      }
    end

    assert_redirected_to passkeys_path
  end

  test "does not remove the last passkey" do
    user = sign_in_with_passkey
    credential = user.passkey_credentials.first

    assert_no_difference -> { user.passkey_credentials.reload.count } do
      delete passkey_path(credential)
    end

    assert_redirected_to passkeys_path
    assert_equal "Add another passkey before removing this one.", flash[:alert]
  end

  test "removes one passkey when another remains" do
    user = sign_in_with_passkey
    credential = user.passkey_credentials.create!(
      webauthn_id: WebAuthn.generate_user_id,
      public_key: "public-key",
      sign_count: 0,
      nickname: "Backup"
    )

    assert_difference -> { user.passkey_credentials.reload.count }, -1 do
      delete passkey_path(credential)
    end

    assert_redirected_to passkeys_path
  end
end
