module Api
  module Kiosk
    # Asset library for kiosks (contracts §1): the desired-state manifest, ranged
    # downloads and per-kiosk progress reports.
    class AssetsController < BaseController
      def index
        touch_current_kiosk!
        assets = current_account.assets.library.ready.order(:id)
        cover_keys = Asset.attached_cover_keys(current_account)
        render json: { assets: assets.map { |asset| asset.as_kiosk_json(base_url: request.base_url, cover_keys: cover_keys) } }
      end

      def download
        asset = current_account.assets.library.ready.find_by(id: params[:id])
        return render json: { error: "not_found" }, status: :not_found if asset.nil?

        path = asset.file_path
        return render json: { error: "file_missing" }, status: :not_found unless path.file?

        total = path.size
        response.headers["Accept-Ranges"] = "bytes"
        response.headers["X-Asset-Sha256"] = asset.sha256.to_s
        response.headers["ETag"] = %("#{asset.sha256}")
        response.headers["Last-Modified"] = asset.updated_at.httpdate
        response.headers["Cache-Control"] = "private, no-transform"
        response.headers["Content-Disposition"] = ActionDispatch::Http::ContentDisposition.format(disposition: "attachment", filename: asset.filename)

        range = ByteRange.parse(request.headers["Range"], total)
        case range
        when :unsatisfiable
          response.headers["Content-Range"] = "bytes */#{total}"
          head :range_not_satisfiable
        when nil
          stream_file(path, 0, total, :ok)
        else
          response.headers["Content-Range"] = "bytes #{range.begin}-#{range.end}/#{total}"
          stream_file(path, range.begin, range.size, :partial_content)
        end
      end

      def update_status
        asset = current_account.assets.find_by(id: params[:id])
        return render json: { error: "not_found" }, status: :not_found if asset.nil?

        state = params[:state].to_s
        unless KioskAsset::STATES.include?(state)
          return render json: { error: "invalid state" }, status: :unprocessable_content
        end

        touch_current_kiosk!
        record = current_kiosk.kiosk_assets.find_or_initialize_by(asset: asset)
        record.assign_attributes(
          state: state,
          bytes_done: params[:bytes_done].to_i.clamp(0, asset.byte_size),
          error: params[:error].presence&.to_s&.truncate(1000)
        )
        record.save!
        head :no_content
      rescue ActiveRecord::RecordNotUnique
        retry
      end

      private

      # Streams `length` bytes from `offset` without loading the file into memory. The
      # body deliberately doesn't respond to #to_path, so Rack::Sendfile/Thruster never
      # take over (they would ignore the range we already applied).
      def stream_file(path, offset, length, http_status)
        response.status = http_status
        response.headers["Content-Type"] = "application/octet-stream"
        response.headers["Content-Length"] = length.to_s
        self.response_body = request.head? ? [] : FileRangeBody.new(path, offset, length)
      end
    end
  end
end
