require "test_helper"

class AuthenticationTest < ActionDispatch::IntegrationTest
  test "redirects to setup before the first passkey exists" do
    get accounts_path

    assert_redirected_to setup_path
  end

  test "requires passkey login after setup" do
    sign_in_with_passkey
    delete logout_path

    get accounts_path
    assert_redirected_to login_path
  end

  test "logs in with an existing passkey" do
    sign_in_with_passkey
    delete logout_path

    post login_options_path
    assert_response :success
    options = JSON.parse(response.body)

    credential = @passkey_client.get(
      challenge: options.fetch("challenge"),
      allow_credentials: options.fetch("allowCredentials").map { |item| item.fetch("id") },
      user_verified: true
    )

    post login_path, params: { public_key_credential: credential.to_json }
    assert_redirected_to accounts_path
  end
end
