require "test_helper"

class LinkedAccountTest < ActiveSupport::TestCase
  include FakeRommTestHelper

  setup do
    @fake = install_fake_romm
    @account = Account.create!(name: "Lobby")
    @linked = link_romm(@account)
  end

  teardown { remove_fake_romm }

  test "credentials are encrypted at rest" do
    raw = LinkedAccount.connection.select_value("SELECT credentials FROM linked_accounts WHERE id = #{@linked.id}")
    assert_not_includes raw, "hunter2"
    assert_not_includes raw, "kim"
    assert_equal "hunter2", @linked.reload.credential_data["password"]
  end

  test "credentials are absent from json, inspect and the summary" do
    [ @linked.to_json, @linked.as_json.to_s, @linked.inspect, @linked.credential_summary ].each do |text|
      assert_not_includes text, "hunter2"
    end
    assert_equal "kim •••", @linked.credential_summary
    assert_not_includes @linked.to_json, "credentials"
  end

  test "blank secrets keep the saved ones when editing" do
    @linked.assign_credentials(auth: "password", username: "", password: "")
    @linked.save!
    assert_equal({ "auth" => "password", "username" => "kim", "password" => "hunter2" }, @linked.reload.credential_data)

    @linked.assign_credentials(auth: "password", username: "other", password: "")
    assert_not @linked.valid?, "a new username needs its password"
  end

  test "an api token is accepted instead of a password" do
    @linked.assign_credentials(auth: "token", token: FakeRomm::API_TOKEN)
    assert @linked.save
    assert_equal "API token •••", @linked.credential_summary
  end

  test "base_url rules" do
    assert_not @account.linked_accounts.new(provider: "romm", label: "x", base_url: "http://romm.example.test", credentials: { auth: "token", token: "t" }.to_json).valid?
    assert @account.linked_accounts.new(provider: "romm", label: "x", base_url: "http://192.168.1.4:8080", allow_local_network: true,
      credentials: { auth: "token", token: "t" }.to_json).valid?
    assert_equal "https://romm.example.test", @account.linked_accounts.new(base_url: " https://romm.example.test/ ").tap(&:valid?).base_url
  end

  test "credentials never reach the log when a sync runs" do
    log = StringIO.new
    logger = ActiveSupport::Logger.new(log)
    original = Rails.logger
    Rails.logger = logger
    @linked.linked_imports.create!(kind: "rom", remote_id: "10", label: "Tiny Quest")
    LinkedSync.new(@linked).call
    @fake.failing_downloads = [ 10 ]
    LinkedSync.new(@linked.reload).call
    Rails.logger = original

    assert_not_includes log.string, "hunter2"
    assert_not_includes log.string, FakeRomm::TOKEN
  end

  test "an account's linked accounts go with it, after its assets" do
    @linked.linked_imports.create!(kind: "rom", remote_id: "10", label: "Tiny Quest")
    LinkedSync.new(@linked).call
    assert_difference -> { LinkedAccount.count }, -1 do
      @account.destroy!
    end
    assert_equal 0, Asset.count
  end
end
