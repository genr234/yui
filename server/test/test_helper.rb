ENV["RAILS_ENV"] ||= "test"
require_relative "../config/environment"
require "rails/test_help"
require "webauthn/fake_client"

module ActiveSupport
  class TestCase
    # Run tests in parallel with specified workers
    parallelize(workers: :number_of_processors)

    # Setup all fixtures in test/fixtures/*.yml for all tests in alphabetical order.
    fixtures :all

    # Add more helper methods to be used by all tests here...
  end
end

module PasskeyTestHelper
  def sign_in_with_passkey(name: "Admin")
    get setup_path
    assert_response :success

    post passkey_options_path, params: { name: name }
    assert_response :success
    creation_options = JSON.parse(response.body)

    @passkey_client = WebAuthn::FakeClient.new("http://www.example.com")
    credential = @passkey_client.create(
      challenge: creation_options.fetch("challenge"),
      user_verified: true
    )

    post passkeys_path, params: {
      name: name,
      public_key_credential: credential.to_json
    }
    assert_redirected_to accounts_path

    User.find_by!(name: name)
  end
end

class ActionDispatch::IntegrationTest
  include PasskeyTestHelper
end
