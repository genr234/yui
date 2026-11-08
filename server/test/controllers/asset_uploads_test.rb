require "test_helper"

class AssetUploadsTest < ActionDispatch::IntegrationTest
  include ActiveJob::TestHelper
  include DiscFixtures

  TUS = { "Tus-Resumable" => "1.0.0" }.freeze

  setup do
    @account = Account.create!(name: "Lobby")
  end

  test "create, patch in two chunks, complete and enqueue processing" do
    sign_in_with_passkey
    data = iso_image(game_id: "RMCE01", title: "MARIO KART WII")
    first, second = data.byteslice(0, 1000), data.byteslice(1000..)

    post account_asset_uploads_path(@account), headers: TUS.merge(
      "Upload-Length" => data.bytesize.to_s,
      "Upload-Metadata" => metadata(filename: "../Mario Kart Wii.iso", kind: "wii-game")
    )
    assert_response :created
    assert_equal "1.0.0", response.headers["Tus-Resumable"]
    location = response.headers["Location"]
    asset = @account.assets.sole
    assert_equal account_asset_upload_url(@account, asset), location
    assert_equal "uploading", asset.status
    assert_equal "Mario Kart Wii.iso", asset.filename
    assert_equal "Mario Kart Wii", asset.title
    assert_equal data.bytesize, asset.byte_size

    patch location, params: first, headers: chunk_headers(0)
    assert_response :no_content
    assert_equal "1000", response.headers["Upload-Offset"]
    assert_equal "uploading", asset.reload.status

    # Resume: the client asks where to continue.
    head location, headers: TUS
    assert_response :ok
    assert_equal "1000", response.headers["Upload-Offset"]
    assert_equal data.bytesize.to_s, response.headers["Upload-Length"]
    assert_equal "no-store", response.headers["Cache-Control"]

    assert_enqueued_with(job: ProcessAssetJob, args: [ asset.id, "http://www.example.com" ]) do
      patch location, params: second, headers: chunk_headers(1000)
    end
    assert_response :no_content
    assert_equal data.bytesize.to_s, response.headers["Upload-Offset"]

    asset.reload
    assert_equal "processing", asset.status
    assert_equal data, File.binread(asset.file_path)

    # A finished upload reports itself complete so a resuming client just succeeds.
    head location, headers: TUS
    assert_equal data.bytesize.to_s, response.headers["Upload-Offset"]

    perform_enqueued_jobs
    assert_equal "ready", asset.reload.status
    assert_equal "RMCE01", asset.metadata["game_id"]
  end

  test "rejects a patch at the wrong offset and keeps the data" do
    sign_in_with_passkey
    location = create_upload(length: 10)

    patch location, params: "12345", headers: chunk_headers(0)
    assert_response :no_content

    patch location, params: "67890", headers: chunk_headers(0)
    assert_response :conflict

    patch location, params: "67890", headers: chunk_headers(5).merge("Content-Type" => "application/json")
    assert_response :unsupported_media_type

    patch location, params: "6789012345", headers: chunk_headers(5)
    assert_response :bad_request

    head location, headers: TUS
    assert_equal "5", response.headers["Upload-Offset"]
  end

  test "runtime metadata comes from the upload form" do
    sign_in_with_passkey
    post account_asset_uploads_path(@account), headers: TUS.merge(
      "Upload-Length" => "3",
      "Upload-Metadata" => metadata(filename: "dolphin-2509.7z", kind: "runtime", title: "Dolphin", runtime: "dolphin", version: "2509")
    )
    assert_response :created

    asset = @account.assets.sole
    assert_equal "Dolphin", asset.title
    assert_equal({ "runtime" => "dolphin", "version" => "2509", "archive" => "7z" }, asset.metadata)
  end

  test "an empty upload completes on creation" do
    sign_in_with_passkey
    assert_enqueued_jobs 1, only: ProcessAssetJob do
      create_upload(length: 0)
    end
    assert_equal "processing", @account.assets.sole.status
  end

  test "validates the creation request" do
    sign_in_with_passkey

    post account_asset_uploads_path(@account), headers: { "Upload-Length" => "10", "Upload-Metadata" => metadata(filename: "a.bin", kind: "other") }
    assert_response :precondition_failed

    post account_asset_uploads_path(@account), headers: TUS.merge("Upload-Metadata" => metadata(filename: "a.bin", kind: "other"))
    assert_response :bad_request

    post account_asset_uploads_path(@account), headers: TUS.merge("Upload-Length" => "10", "Upload-Metadata" => metadata(filename: "a.bin", kind: "console"))
    assert_response :bad_request

    post account_asset_uploads_path(@account), headers: TUS.merge("Upload-Length" => (AssetUploadsController.max_size + 1).to_s, "Upload-Metadata" => metadata(filename: "a.bin", kind: "other"))
    assert_response :content_too_large

    assert_equal 0, Asset.count
  end

  test "terminating an upload deletes it" do
    sign_in_with_passkey
    location = create_upload(length: 10)
    patch location, params: "12345", headers: chunk_headers(0)
    asset = @account.assets.sole

    delete location, headers: TUS
    assert_response :no_content
    assert_not Asset.exists?(asset.id)
    assert_not asset.directory.exist?
  end

  test "advertises tus capabilities" do
    process :options, account_asset_uploads_path(@account)
    assert_response :no_content
    assert_equal "1.0.0", response.headers["Tus-Version"]
    assert_equal "creation,termination", response.headers["Tus-Extension"]
  end

  test "requires a signed-in user" do
    sign_in_with_passkey
    location = create_upload(length: 10)
    delete logout_path

    post account_asset_uploads_path(@account), headers: TUS.merge("Upload-Length" => "10", "Upload-Metadata" => metadata(filename: "a.bin", kind: "other"))
    assert_response :unauthorized
    head location, headers: TUS
    assert_response :unauthorized
    patch location, params: "12345", headers: chunk_headers(0)
    assert_response :unauthorized
    assert_equal 0, @account.assets.sole.stored_bytes
  end

  test "uploads are scoped to the account in the URL" do
    sign_in_with_passkey
    location = create_upload(length: 10)
    other = Account.create!(name: "Other")
    asset = @account.assets.sole

    head account_asset_upload_path(other, asset), headers: TUS
    assert_response :not_found
    patch account_asset_upload_path(other, asset), params: "12345", headers: chunk_headers(0)
    assert_response :not_found
    assert_equal location, account_asset_upload_url(@account, asset)
  end

  private

  def create_upload(length:)
    post account_asset_uploads_path(@account), headers: TUS.merge(
      "Upload-Length" => length.to_s,
      "Upload-Metadata" => metadata(filename: "file.bin", kind: "other")
    )
    assert_response :created
    response.headers["Location"]
  end

  def chunk_headers(offset)
    TUS.merge("Upload-Offset" => offset.to_s, "Content-Type" => "application/offset+octet-stream")
  end

  def metadata(**values)
    values.map { |key, value| "#{key} #{Base64.strict_encode64(value)}" }.join(",")
  end
end
