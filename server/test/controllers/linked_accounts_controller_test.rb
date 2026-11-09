require "test_helper"

class LinkedAccountsControllerTest < ActionDispatch::IntegrationTest
  include FakeRommTestHelper

  setup do
    @fake = install_fake_romm
    sign_in_with_passkey
    @account = Account.create!(name: "Lobby")
  end

  teardown { remove_fake_romm }

  def link_params(overrides = {})
    { linked_account: { label: "Home RomM", base_url: "https://romm.example.test", auth: "password", username: "kim", password: "hunter2" }.merge(overrides) }
  end

  test "link RomM: the form, then save lands on the import picker" do
    get new_account_linked_account_path(@account)
    assert_response :success
    assert_select "form.linked-form"
    assert_select "button", text: "Test connection"

    assert_difference -> { @account.linked_accounts.count } do
      post account_linked_accounts_path(@account), params: link_params
    end
    linked = @account.linked_accounts.last
    assert_redirected_to account_linked_account_path(@account, linked)
    assert_equal "kim", linked.credential_data["username"]
  end

  test "an unsafe address is refused when saving" do
    post account_linked_accounts_path(@account), params: link_params(base_url: "http://romm.example.test")
    assert_response :unprocessable_content
    assert_select ".form-errors", /https/
    assert_equal 0, @account.linked_accounts.count
  end

  test "test connection shows the version and game count, and plain errors" do
    post test_account_linked_accounts_path(@account), params: link_params
    assert_response :success
    assert_select "turbo-frame#linked_test_result .linked-test--ok", /RomM 4\.9\.2 with 3 games on 2 platforms/

    post test_account_linked_accounts_path(@account), params: link_params(password: "wrong")
    assert_select ".linked-test--error", "RomM rejected the login"

    @fake.version = "9.0.0"
    post test_account_linked_accounts_path(@account), params: link_params
    assert_select ".linked-test--error", "RomM's API changed (version 9.0.0); Yui supports 4.x to 5.x"

    LinkedProviders::HttpClient.resolver = ->(_host) { [ "127.0.0.1" ] }
    post test_account_linked_accounts_path(@account), params: link_params
    assert_select ".linked-test--error", /isn't allowed/
    assert_equal 0, @account.linked_accounts.count, "testing never saves"
  end

  test "editing never shows the saved secrets and blank fields keep them" do
    linked = link_romm(@account)
    get edit_account_linked_account_path(@account, linked)
    assert_response :success
    assert_not_includes response.body, "hunter2"
    assert_select "input[name='linked_account[password]'][value]", count: 0
    assert_select "strong", "kim •••"

    patch account_linked_account_path(@account, linked), params: { linked_account: { label: "Renamed", base_url: "https://romm.example.test", auth: "password", username: "", password: "" } }
    assert_redirected_to account_path(@account, anchor: "linked-accounts")
    linked.reload
    assert_equal "Renamed", linked.label
    assert_equal "hunter2", linked.credential_data["password"]

    post test_account_linked_account_path(@account, linked), params: { linked_account: { label: "Renamed", base_url: "https://romm.example.test", auth: "password", username: "", password: "" } }
    assert_select ".linked-test--ok"
  end

  test "the account page lists linked accounts with status, summary and a badge on imported games" do
    linked = link_romm(@account)
    linked.linked_imports.create!(kind: "platform", remote_id: "1", label: "Super Nintendo")
    LinkedSync.new(linked).call

    get account_path(@account)
    assert_response :success
    assert_not_includes response.body, "hunter2"
    assert_select "#linked-accounts" do
      assert_select ".status-dot--ok"
      assert_select "strong", /Home RomM/
      assert_select "small", text: "2 games from 1 platform. 164 Bytes."
      assert_select "a", "Link RomM"
    end
    assert_select "turbo-frame#asset_library" do
      assert_select ".source-badge", text: "RomM", count: 2
      assert_select "img[src^='/covers/linked/']", 2
      assert_select "form[action^='/accounts/#{@account.id}/assets/']", count: 0
    end
  end

  test "an error status shows the reason on the card" do
    linked = link_romm(@account)
    linked.finish_sync!(error: "RomM rejected the login")
    get account_path(@account)
    assert_select ".status-dot--error"
    assert_select ".asset-row__error", "RomM rejected the login"
  end

  test "the import picker lists platforms, collections and search results, and toggles imports" do
    linked = link_romm(@account)

    get account_linked_account_path(@account, linked)
    assert_response :success
    assert_select ".linked-toggle strong", "Super Nintendo"
    assert_select ".linked-toggle small", "2 games · snes"

    assert_difference -> { linked.linked_imports.count } do
      post account_linked_account_linked_imports_path(@account, linked), params: { kind: "platform", remote_id: "1", label: "Super Nintendo", tab: "platforms" }
    end
    assert_redirected_to account_linked_account_path(@account, linked, tab: "platforms")

    get account_linked_account_path(@account, linked)
    assert_select "input[type=checkbox][checked]", 1

    assert_difference -> { linked.linked_imports.count }, -1 do
      delete account_linked_account_linked_import_path(@account, linked, linked.linked_imports.first)
    end

    get account_linked_account_path(@account, linked, tab: "collections")
    assert_select ".linked-toggle strong", "Favourites"

    get account_linked_account_path(@account, linked, tab: "search", q: "tiny")
    assert_select ".linked-results .linked-toggle strong", "Tiny Quest"
    assert_select ".linked-results img[src='#{cover_account_linked_account_path(@account, linked, remote_id: 10)}']"
  end

  test "the cover proxy serves the cover without exposing credentials" do
    linked = link_romm(@account)
    get cover_account_linked_account_path(@account, linked, remote_id: 10)
    assert_response :success
    assert_equal "image/png", response.media_type
    get cover_account_linked_account_path(@account, linked, remote_id: "abc")
    assert_response :not_found
  end

  test "sync now queues a job once" do
    linked = link_romm(@account)
    assert_enqueued_with(job: LinkedAccountSyncJob, args: [ linked.id ]) do
      post sync_account_linked_account_path(@account, linked)
    end
    assert_redirected_to account_path(@account, anchor: "linked-accounts")

    linked.update!(status: "syncing", syncing_since: Time.current)
    assert_no_enqueued_jobs only: LinkedAccountSyncJob do
      post sync_account_linked_account_path(@account, linked)
    end
  end

  test "unlink and keep leaves the games as ordinary assets" do
    linked = link_romm(@account)
    linked.linked_imports.create!(kind: "platform", remote_id: "1", label: "Super Nintendo")
    LinkedSync.new(linked).call
    game = @account.assets.first

    get account_path(@account)
    assert_select "details.linked-unlink" do
      assert_select "form[action*='assets=keep'] button", "Keep the games"
      assert_select "form[action*='assets=remove'] button", "Remove the games"
    end

    assert_difference -> { LinkedAccount.count }, -1 do
      delete account_linked_account_path(@account, linked, assets: "keep")
    end
    assert_equal 2, @account.assets.count
    game.reload
    assert_nil game.linked_account_id
    assert_not game.metadata.key?("source")
    assert_not game.imported?
    assert_equal 0, LinkedImport.count
  end

  test "unlink and remove deletes the games and their covers" do
    linked = link_romm(@account)
    linked.linked_imports.create!(kind: "platform", remote_id: "1", label: "Super Nintendo")
    LinkedSync.new(linked).call
    cover = LinkedCover.cached(File.basename(@account.assets.first.metadata["cover_url"]))

    delete account_linked_account_path(@account, linked, assets: "remove")
    assert_equal 0, @account.assets.count
    assert_equal 0, LinkedAccount.count
    assert_not cover.exist?
  end

  test "changes need a recent passkey confirmation" do
    linked = link_romm(@account)
    travel 11.minutes do
      delete account_linked_account_path(@account, linked, assets: "keep")
      assert_redirected_to login_path
    end
    assert LinkedAccount.exists?(linked.id)
  end

  test "linked covers are served publicly with an image type, and only known keys" do
    linked = link_romm(@account)
    linked.linked_imports.create!(kind: "rom", remote_id: "10", label: "Tiny Quest")
    LinkedSync.new(linked).call
    url = @account.assets.first.metadata["cover_url"]

    get url
    assert_response :success
    assert_equal "image/png", response.media_type

    get "/covers/linked/999-00000000.png"
    assert_response :not_found
    get "/covers/linked/..%2Fsecrets"
    assert_response :not_found
  end

  test "imported games can't be deleted from the library" do
    linked = link_romm(@account)
    linked.linked_imports.create!(kind: "rom", remote_id: "10", label: "Tiny Quest")
    LinkedSync.new(linked).call
    get account_assets_path(@account)
    assert_select ".asset-row__actions small", "Managed by sync"

    assert_no_difference -> { Asset.count } do
      delete account_asset_path(@account, @account.assets.first)
    end
    assert_match(/managed by RomM/, flash[:alert])
  end
end
