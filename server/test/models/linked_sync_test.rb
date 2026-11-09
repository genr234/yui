require "test_helper"

class LinkedSyncTest < ActiveSupport::TestCase
  include ActiveJob::TestHelper
  include FakeRommTestHelper

  setup do
    @fake = install_fake_romm
    @account = Account.create!(name: "Lobby")
    @kiosk = @account.kiosks.create!(name: "Front", device_uid: "front", device_token_digest: Kiosk.digest_token("t"))
    @linked = link_romm(@account)
  end

  teardown { remove_fake_romm }

  def sync
    LinkedSync.new(@linked.reload).tap(&:call)
  end

  def fetch(asset)
    perform_enqueued_jobs(only: LinkedFetchJob) { LinkedFetch.request(asset) }
    asset.reload
  end

  test "importing a platform lists games without downloading them" do
    @linked.linked_imports.create!(kind: "platform", remote_id: "1", label: "Super Nintendo")

    result = sync.result
    assert_equal 2, result.added
    assert_equal "ok", @linked.reload.status
    assert_nil @linked.last_error
    assert_not_nil @linked.last_synced_at
    assert_equal 0, @fake.calls.count { |call| call.include?("/content/") }

    asset = @account.assets.find_by!(title: "Tiny Quest")
    assert_equal "game", asset.kind
    assert asset.remote?
    assert_nil asset.sha256
    assert_not asset.file_path.exist?
    assert_equal "tiny-quest.sfc", asset.filename
    assert_equal 64, asset.byte_size
    assert_equal "snes", asset.metadata["platform"]
    assert_equal "USA", asset.metadata["region"]
    assert_equal "unknown", asset.metadata["format"]
    assert_equal({ "provider" => "romm", "linked_account_id" => @linked.id, "remote_id" => "10", "file" => "tiny-quest.sfc", "remote_size" => 64 },
      asset.metadata["source"])
    assert asset.imported?

    cover_url = asset.metadata["cover_url"]
    assert_match %r{\A/covers/linked/#{asset.id}-\h{8}\.png\z}, cover_url
    assert LinkedCover.cached(File.basename(cover_url))
    assert_equal "folder-game.zip", @account.assets.find_by!(title: "Folder Game").filename
  end

  test "the kiosk manifest lists remote games with absolute cover urls and a remote flag" do
    @linked.linked_imports.create!(kind: "rom", remote_id: "12", label: "Blast Test")
    sync
    asset = @account.assets.find_by!(title: "Blast Test")

    json = asset.as_kiosk_json(base_url: "https://yui.example.test")
    assert_equal "genesis", json[:metadata]["platform"]
    assert_match %r{\Ahttps://yui.example.test/covers/linked/}, json[:metadata]["cover_url"]
    assert json[:remote]
    assert_equal "remote", json[:remote_state]
    assert_includes @account.assets.library.listed, asset
  end

  test "a request fetches the file, processes it and tells kiosks" do
    @linked.linked_imports.create!(kind: "rom", remote_id: "10", label: "Tiny Quest")
    sync
    asset = @account.assets.first

    assert_difference -> { @kiosk.kiosk_commands.where(command_type: "assets.reconcile").count } do
      fetch(asset)
    end
    assert asset.ready?
    assert_equal Digest::SHA256.hexdigest(@fake.rom_bytes(10)), asset.sha256
    assert_equal @fake.rom_bytes(10), asset.file_path.binread
    assert_equal "snes", asset.metadata["platform"]
    assert_nil asset.as_kiosk_json[:remote]

    # Asking again for a ready game downloads nothing.
    assert_no_difference -> { @fake.calls.count { |call| call.include?("/content/") } } do
      assert_equal "ready", LinkedFetch.request(asset)
    end
  end

  test "a request only queues one fetch" do
    @linked.linked_imports.create!(kind: "rom", remote_id: "10", label: "Tiny Quest")
    sync
    asset = @account.assets.first
    assert_enqueued_jobs 1, only: LinkedFetchJob do
      assert_equal "fetching", LinkedFetch.request(asset)
      assert_equal "fetching", LinkedFetch.request(asset.reload)
    end
  end

  test "importing queues a reconcile for the account's kiosks" do
    @linked.linked_imports.create!(kind: "rom", remote_id: "10", label: "Tiny Quest")
    assert_difference -> { @kiosk.kiosk_commands.where(command_type: "assets.reconcile").count } do
      sync
    end
  end

  test "collections and single roms import, and a second sync changes nothing" do
    @linked.linked_imports.create!(kind: "collection", remote_id: "5", label: "Favourites")
    @linked.linked_imports.create!(kind: "rom", remote_id: "10", label: "Tiny Quest")

    assert_equal 2, sync.result.added
    ids = @account.assets.pluck(:id).sort
    again = nil
    assert_no_difference -> { @kiosk.kiosk_commands.count } do
      again = sync.result
    end
    assert_equal [ 0, 0, 0, 0 ], [ again.added, again.updated, again.removed, again.failed ]
    assert_equal ids, @account.assets.pluck(:id).sort
  end

  test "a changed file drops the fetched copy and goes back to remote" do
    @linked.linked_imports.create!(kind: "rom", remote_id: "10", label: "Tiny Quest")
    sync
    asset = fetch(@account.assets.first)
    path = asset.file_path
    assert path.exist?
    @fake.instance_variable_get(:@roms).first["fs_size_bytes"] = 60
    @fake.instance_variable_get(:@roms).first["files"][0]["file_size_bytes"] = 60

    assert_equal 1, sync.result.updated
    asset.reload
    assert asset.remote?
    assert_equal 60, asset.byte_size
    assert_nil asset.sha256
    assert_not path.exist?
    assert fetch(asset).ready?
    assert_equal 60, asset.byte_size
  end

  test "removing an import deletes its assets at the next sync" do
    import = @linked.linked_imports.create!(kind: "platform", remote_id: "1", label: "Super Nintendo")
    @linked.linked_imports.create!(kind: "rom", remote_id: "12", label: "Blast Test")
    sync
    assert_equal 3, @account.assets.count
    asset = fetch(@account.assets.find_by!(title: "Tiny Quest"))
    directory = asset.directory
    cover = LinkedCover.cached(File.basename(asset.metadata["cover_url"]))
    assert directory.directory?

    import.destroy!
    assert_equal 2, sync.result.removed
    assert_equal [ "Blast Test" ], @account.assets.pluck(:title)
    assert_not directory.exist?
    assert_not cover.exist?
  end

  test "a failed fetch goes back to remote with the reason and can be asked for again" do
    @linked.linked_imports.create!(kind: "rom", remote_id: "10", label: "Tiny Quest")
    sync
    @fake.failing_downloads = [ 10 ]
    asset = fetch(@account.assets.first)
    assert asset.remote?
    assert_predicate asset.error, :present?
    assert_not asset.file_path.exist?
    assert_equal asset.error, asset.as_kiosk_json[:remote_error]

    @fake.failing_downloads = []
    assert fetch(asset).ready?
    assert_nil asset.error
  end

  test "downloads are retried with backoff before giving up" do
    @linked.linked_imports.create!(kind: "rom", remote_id: "10", label: "Tiny Quest")
    sync
    LinkedFetch.backoff = [ 0, 0 ]
    @fake.failing_downloads = [ 10 ]
    fetch(@account.assets.first)
    assert_equal 3, @fake.calls.count { |call| call.include?("/content/") }
  end

  test "a login failure stops the sync, shows the reason and removes nothing" do
    @linked.linked_imports.create!(kind: "rom", remote_id: "10", label: "Tiny Quest")
    sync
    @linked.reload.assign_credentials(auth: "password", username: "kim", password: "changed")
    @linked.save!

    assert_equal false, LinkedSync.new(@linked).call
    @linked.reload
    assert_equal "error", @linked.status
    assert_equal "RomM rejected the login", @linked.last_error
    assert_equal 1, @account.assets.count
  end

  test "an unreachable RomM never deletes games" do
    @linked.linked_imports.create!(kind: "rom", remote_id: "10", label: "Tiny Quest")
    sync
    LinkedProviders::HttpClient.resolver = ->(_host) { raise SocketError }
    sync
    assert_equal "error", @linked.reload.status
    assert_match(/Couldn't reach/, @linked.last_error)
    assert_equal 1, @account.assets.count
  end

  test "a sync that is already running is not started twice" do
    @linked.update!(status: "syncing", syncing_since: Time.current)
    assert_equal false, LinkedSync.new(@linked).call
    assert_empty @fake.requests
  end

  test "a stale syncing marker is reclaimed" do
    @linked.update!(status: "syncing", syncing_since: 3.hours.ago)
    assert LinkedSync.new(@linked).call
    assert_equal "ok", @linked.reload.status
  end

  test "the job and the recurring job" do
    @linked.linked_imports.create!(kind: "rom", remote_id: "10", label: "Tiny Quest")
    LinkedAccountSyncJob.perform_now(@linked.id)
    assert_equal 1, @account.assets.count

    other = link_romm(Account.create!(name: "Empty"))
    assert_enqueued_jobs 1, only: LinkedAccountSyncJob do
      LinkedAccountsSyncJob.perform_now
    end
    assert_not_nil other
  end
end
