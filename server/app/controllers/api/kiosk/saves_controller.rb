require "digest"
require "tempfile"

module Api
  module Kiosk
    # Save-file backups for native sessions (plan 08 item 5). A backup is one `other` asset
    # per account and "<app id>/<runtime id>" key; uploading replaces the previous one.
    # The app only says which paths are saves; nothing here knows what the program is.
    class SavesController < BaseController
      MAX_BYTES = ENV.fetch("SAVE_BACKUP_MAX_BYTES", 64.megabytes).to_i
      MAX_KEYS_PER_ACCOUNT = 200
      APP_ID = /\A[a-z0-9][a-z0-9._-]{1,126}[a-z0-9_-]\z/
      RUNTIME_ID = /\A[A-Za-z0-9][A-Za-z0-9_.-]{0,63}\z/
      ZIP_MAGIC = "PK".b

      before_action :load_key

      def show
        backup = find_backup
        return render json: { error: "not_found" }, status: :not_found if backup.nil?

        render json: backup_json(backup)
      end

      def download
        backup = find_backup
        return render json: { error: "not_found" }, status: :not_found if backup.nil?
        return render json: { error: "file_missing" }, status: :not_found unless backup.file_path.file?

        response.headers["X-Asset-Sha256"] = backup.sha256.to_s
        send_file backup.file_path, type: "application/zip", disposition: "attachment", filename: backup.filename
      end

      def update
        saved_at = parse_saved_at(params[:saved_at])
        return render json: { error: "invalid saved_at" }, status: :unprocessable_content if saved_at.nil?

        declared = request.content_length
        if declared && declared > MAX_BYTES
          return render json: { error: "too_large", max_bytes: MAX_BYTES }, status: :content_too_large
        end

        upload = receive_body
        return if upload.nil?

        begin
          expected = request.headers["X-Save-Sha256"].to_s.downcase.presence
          if expected && expected != upload[:sha256]
            return render json: { error: "sha256_mismatch" }, status: :unprocessable_content
          end

          touch_current_kiosk!
          backup = store_backup(upload, saved_at)
          render json: backup_json(backup) if backup
        ensure
          upload[:file].close!
        end
      end

      private

      def load_key
        @app_id = params[:app_id].to_s
        @runtime = params[:runtime].to_s
        return if APP_ID.match?(@app_id) && RUNTIME_ID.match?(@runtime)

        render json: { error: "invalid key" }, status: :unprocessable_content
      end

      def save_key
        "#{@app_id}/#{@runtime}"
      end

      def find_backup
        current_account.assets.save_backups.ready.find_by(save_key: save_key)
      end

      def backup_json(backup)
        {
          id: backup.id.to_s,
          key: backup.save_key,
          byte_size: backup.byte_size,
          sha256: backup.sha256,
          saved_at: backup.metadata["saved_at"],
          metadata: backup.metadata
        }
      end

      # ISO 8601 UTC with second precision, so string comparison orders times.
      def parse_saved_at(value)
        time = Time.iso8601(value.to_s)
        [ time, Time.current ].min.utc.iso8601
      rescue ArgumentError
        nil
      end

      # Copies the body to a tempfile, counting bytes as they arrive so a lying
      # Content-Length (or a chunked body) cannot exceed the cap.
      def receive_body
        file = Tempfile.new([ "save-backup", ".zip" ], binmode: true)
        digest = Digest::SHA256.new
        total = 0
        head = "".b
        body = request.body
        while (chunk = body.read(64.kilobytes))
          total += chunk.bytesize
          if total > MAX_BYTES
            file.close!
            render json: { error: "too_large", max_bytes: MAX_BYTES }, status: :content_too_large
            return nil
          end
          head << chunk.byteslice(0, 2 - head.bytesize) if head.bytesize < 2
          digest.update(chunk)
          file.write(chunk)
        end
        file.flush
        if total.zero? || head != ZIP_MAGIC
          file.close!
          render json: { error: "not a zip archive" }, status: :unprocessable_content
          return nil
        end
        { file: file, sha256: digest.hexdigest, bytes: total }
      end

      def store_backup(upload, saved_at)
        attempts = 0
        begin
          replace_backup(upload, saved_at)
        rescue ActiveRecord::RecordNotUnique
          attempts += 1
          retry if attempts < 2
          raise
        end
      end

      # Returns the backup, or nil after rendering why it was refused.
      def replace_backup(upload, saved_at)
        Asset.transaction do
          backup = current_account.assets.save_backups.find_by(save_key: save_key)
          if backup.nil? && current_account.assets.save_backups.count >= MAX_KEYS_PER_ACCOUNT
            render json: { error: "too_many_backups" }, status: :unprocessable_content
            return nil
          end
          # An older kiosk never clobbers a newer backup.
          if backup && backup.metadata["saved_at"].to_s > saved_at
            render json: { error: "newer_backup_exists", saved_at: backup.metadata["saved_at"] }, status: :conflict
            return nil
          end

          backup ||= current_account.assets.new(save_key: save_key, kind: "other", filename: "saves.zip")
          backup.assign_attributes(
            title: "Save data: #{@app_id}".truncate(200),
            byte_size: upload[:bytes],
            sha256: upload[:sha256],
            status: "ready",
            error: nil,
            metadata: {
              "role" => "save_backup",
              "app_id" => @app_id,
              "runtime" => @runtime,
              "saved_at" => saved_at,
              "kiosk_name" => current_kiosk.name,
              "sha256" => upload[:sha256],
              "size" => upload[:bytes]
            }
          )
          backup.save!
          install_file(backup, upload[:file])
          backup
        end
      end

      # Moves the new bytes into place with a rename, so a reader sees the old
      # or the new backup, never half of one.
      def install_file(backup, tempfile)
        FileUtils.mkdir_p(backup.directory)
        staged = backup.directory.join(".incoming-#{SecureRandom.hex(4)}")
        FileUtils.cp(tempfile.path, staged)
        File.rename(staged, backup.file_path)
      ensure
        FileUtils.rm_f(staged) if staged
      end
    end
  end
end
