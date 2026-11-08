# Resumable uploads for the asset library, speaking the core tus 1.0 protocol
# (https://tus.io/protocols/resumable-upload) plus the creation and termination
# extensions. Data is appended straight to the asset's final path on disk, so a
# multi-GB upload is never copied.
#
#   OPTIONS /accounts/:account_id/uploads       capabilities
#   POST    /accounts/:account_id/uploads       create (Upload-Length, Upload-Metadata)
#   HEAD    /accounts/:account_id/uploads/:id   current Upload-Offset (resume)
#   PATCH   /accounts/:account_id/uploads/:id   append at Upload-Offset
#   DELETE  /accounts/:account_id/uploads/:id   terminate
#
# Upload-Metadata keys: filename (required), kind (required, one of Asset::KINDS),
# title, and for runtimes runtime + version.
class AssetUploadsController < ApplicationController
  TUS_VERSION = "1.0.0".freeze
  TUS_EXTENSIONS = "creation,termination".freeze
  CONTENT_TYPE = "application/offset+octet-stream".freeze
  COPY_CHUNK = 1.megabyte

  # Browser sessions only; respond with status codes rather than login redirects so
  # tus-js-client reports a useful error.
  skip_before_action :require_control_room_access
  skip_forgery_protection only: :options
  before_action :set_tus_headers
  before_action :require_signed_in_user, except: :options
  before_action :require_tus_resumable, except: :options
  before_action :set_account, except: :options
  before_action :set_asset, only: [ :show, :update, :destroy ]

  def self.max_size
    ENV.fetch("ASSET_MAX_UPLOAD_BYTES", 128.gigabytes).to_i
  end

  def options
    response.headers["Tus-Version"] = TUS_VERSION
    response.headers["Tus-Extension"] = TUS_EXTENSIONS
    response.headers["Tus-Max-Size"] = self.class.max_size.to_s
    head :no_content
  end

  def create
    length = integer_header("Upload-Length")
    return tus_error(:bad_request, "Upload-Length is required") if length.nil?
    return tus_error(:content_too_large, "Upload is larger than #{self.class.max_size} bytes") if length > self.class.max_size

    metadata = parse_metadata(request.headers["Upload-Metadata"])
    kind = metadata["kind"].presence || "other"
    return tus_error(:bad_request, "Unknown kind") unless Asset::KINDS.include?(kind)

    filename = Asset.sanitize_filename(metadata["filename"].presence || metadata["name"])
    asset = @account.assets.new(
      kind: kind,
      filename: filename,
      title: metadata["title"].to_s.strip.truncate(200).presence || Asset.default_title(filename),
      byte_size: length,
      status: "uploading",
      metadata: kind == "runtime" ? runtime_metadata(metadata, filename) : {}
    )
    return tus_error(:bad_request, asset.errors.full_messages.to_sentence) unless asset.save

    asset.create_empty_file!
    complete!(asset) if length.zero?

    response.headers["Location"] = account_asset_upload_url(@account, asset)
    response.headers["Upload-Offset"] = "0"
    head :created
  end

  # HEAD (and GET) report how much of the upload the server has.
  def show
    response.headers["Upload-Offset"] = current_offset.to_s
    response.headers["Upload-Length"] = @asset.byte_size.to_s
    response.headers["Cache-Control"] = "no-store"
    head :ok
  end

  def update
    return tus_error(:unsupported_media_type, "Content-Type must be #{CONTENT_TYPE}") unless request.media_type == CONTENT_TYPE

    offset = integer_header("Upload-Offset")
    return tus_error(:bad_request, "Upload-Offset is required") if offset.nil?
    return tus_error(:forbidden, "Upload is already complete") unless @asset.uploading?

    result = append_body(offset)
    return tus_error(:conflict, "Upload-Offset does not match (server has #{result[:offset]})") if result[:conflict]
    return tus_error(:bad_request, "Body exceeds Upload-Length") if result[:overflow]

    @asset.touch
    complete!(@asset) if result[:offset] == @asset.byte_size

    response.headers["Upload-Offset"] = result[:offset].to_s
    head :no_content
  end

  def destroy
    return tus_error(:forbidden, "Only unfinished uploads can be terminated") unless @asset.uploading? || @asset.failed?

    @asset.destroy!
    head :no_content
  end

  private

  def set_tus_headers
    response.headers["Tus-Resumable"] = TUS_VERSION
  end

  def require_signed_in_user
    head :unauthorized unless signed_in?
  end

  def require_tus_resumable
    return if request.headers["Tus-Resumable"] == TUS_VERSION

    response.headers["Tus-Version"] = TUS_VERSION
    head :precondition_failed
  end

  def set_account
    @account = Account.find(params[:account_id])
  end

  def set_asset
    @asset = @account.assets.find(params[:id])
  end

  def current_offset
    @asset.uploading? ? @asset.stored_bytes : @asset.byte_size
  end

  # Appends the request body at `offset`, holding an exclusive lock so two PATCHes for
  # the same upload can't interleave. Never reads more than the remaining length.
  def append_body(offset)
    File.open(@asset.file_path, File::RDWR | File::CREAT | File::BINARY) do |file|
      file.flock(File::LOCK_EX)
      current = file.size
      return { conflict: true, offset: current } unless offset == current

      remaining = @asset.byte_size - current
      file.seek(current)
      body = request.body
      buffer = +""
      written = 0
      overflow = false
      begin
        while body.read(COPY_CHUNK, buffer)
          if written + buffer.bytesize > remaining
            overflow = true
            break
          end
          file.write(buffer)
          written += buffer.bytesize
        end
      rescue IOError, SystemCallError => error
        # The client went away mid-chunk: keep what arrived so it can resume from there.
        Rails.logger.info("[tus] upload #{@asset.id} interrupted: #{error.class}")
      end
      file.flush

      if overflow
        file.truncate(current)
        return { overflow: true, offset: current }
      end

      { offset: current + written }
    end
  end

  def complete!(asset)
    asset.update!(status: "processing")
    ProcessAssetJob.perform_later(asset.id, request.base_url)
  end

  def runtime_metadata(metadata, filename)
    archive = File.extname(filename).delete_prefix(".").downcase
    {
      "runtime" => metadata["runtime"].to_s.strip.first(64).presence,
      "version" => metadata["version"].to_s.strip.first(64).presence,
      "archive" => (%w[zip 7z].include?(archive) ? archive : nil)
    }
  end

  # "key base64value,key2 base64value2" -> { "key" => "value", ... }
  def parse_metadata(header)
    header.to_s.split(",").each_with_object({}) do |pair, result|
      key, value = pair.strip.split(" ", 2)
      next if key.blank?

      decoded = value.present? ? Base64.strict_decode64(value.strip) : ""
      result[key] = decoded.force_encoding(Encoding::UTF_8).scrub("")
    rescue ArgumentError
      next
    end
  end

  def integer_header(name)
    value = request.headers[name].to_s
    Integer(value, 10) if value.match?(/\A\d{1,20}\z/)
  end

  def tus_error(status, message)
    render plain: message, status: status
  end
end
