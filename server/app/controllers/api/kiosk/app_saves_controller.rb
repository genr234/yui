require "digest"
require "tempfile"

module Api
  module Kiosk
    # Opaque save blobs an app keeps for the kiosk's account, or for a player's
    # account when `?player_id=` names a player in the active session who allowed
    # the app (plan 11, contracts §6 "App saves"). Nothing here knows what a blob is.
    class AppSavesController < BaseController
      before_action :validate_app_id
      before_action :validate_key, only: %i[show update destroy]
      before_action :resolve_scope

      def index
        saves = @account.app_saves.where(app_id: params[:app_id], scope: @scope).order(:key)
        render json: { saves: saves.map(&:summary_json) }
      end

      def show
        save = find_save
        return render json: { error: "not_found" }, status: :not_found if save.nil?
        return render json: { error: "file_missing" }, status: :not_found unless save.file_path.file?

        response.headers["X-Save-Sha256"] = save.sha256
        response.headers["X-Updated-At"] = save.saved_at.utc.iso8601
        send_file save.file_path, type: "application/octet-stream", disposition: "attachment", filename: save.key
      end

      def update
        updated_at = parse_updated_at(request.headers["X-Updated-At"])
        return render json: { error: "invalid_updated_at" }, status: :unprocessable_content if updated_at.nil?

        declared = request.content_length
        return too_large if declared && declared > AppSave::MAX_BYTES

        upload = receive_body
        return if upload.nil?

        begin
          touch_current_kiosk!
          save = store(upload, updated_at)
          render json: save.summary_json if save
        ensure
          upload[:file].close!
        end
      end

      def destroy
        find_save&.destroy!
        head :no_content
      end

      private

      def validate_app_id
        return if params[:app_id].to_s.match?(ProfileGrant::APP_ID_FORMAT)

        render json: { error: "invalid_app_id" }, status: :unprocessable_content
      end

      def validate_key
        return if AppSave.valid_key?(params[:key].to_s)

        render json: { error: "invalid_key" }, status: :unprocessable_content
      end

      # Kiosk scope: the kiosk's account. Player scope: the account the player plays
      # as, once the player is in an active session of this kiosk and has granted the app.
      def resolve_scope
        return use_scope(current_account, "kiosk") unless params.key?(:player_id)

        sessions = current_kiosk.play_sessions.active
        player = Player.active.find_by(id: params[:player_id], play_session_id: sessions.select(:id))
        return render json: { error: "not_found" }, status: :not_found unless player

        account = player.profile_account
        return render json: { error: "not_linked" }, status: :conflict unless account
        return render json: { error: "not_granted" }, status: :forbidden unless account.profile_grants.exists?(app_id: params[:app_id])

        use_scope(account, "player")
      end

      def use_scope(account, scope)
        @account = account
        @scope = scope
      end

      def find_save
        @account.app_saves.find_by(app_id: params[:app_id], scope: @scope, key: params[:key])
      end

      def too_large
        render json: { error: "too_large", max_bytes: AppSave::MAX_BYTES }, status: :content_too_large
      end

      # ISO 8601 UTC with second precision, so comparing times is exact. A missing
      # header means now; a future value is clamped to now.
      def parse_updated_at(value)
        now = Time.current.utc.change(usec: 0)
        return now if value.blank?

        [ Time.iso8601(value.to_s).utc.change(usec: 0), now ].min
      rescue ArgumentError
        nil
      end

      # Copies the body to a tempfile, counting bytes as they arrive so a lying
      # Content-Length (or a chunked body) cannot exceed the cap.
      def receive_body
        file = Tempfile.new("app-save", binmode: true)
        digest = Digest::SHA256.new
        total = 0
        body = request.body
        while (chunk = body.read(64.kilobytes))
          total += chunk.bytesize
          if total > AppSave::MAX_BYTES
            file.close!
            too_large
            return nil
          end
          digest.update(chunk)
          file.write(chunk)
        end
        file.flush
        { file: file, sha256: digest.hexdigest, bytes: total }
      end

      def store(upload, updated_at)
        attempts = 0
        begin
          replace(upload, updated_at)
        rescue ActiveRecord::RecordNotUnique
          attempts += 1
          retry if attempts < 2
          raise
        end
      end

      # Returns the save, or nil after rendering why it was refused.
      def replace(upload, updated_at)
        AppSave.transaction do
          scoped = @account.app_saves.where(app_id: params[:app_id], scope: @scope)
          save = scoped.find_by(key: params[:key])
          if save.nil? && scoped.count >= AppSave::MAX_KEYS
            render json: { error: "too_many", max_keys: AppSave::MAX_KEYS }, status: :conflict
            return nil
          end
          # An older writer never clobbers a newer blob.
          if save && save.saved_at > updated_at
            render json: { error: "newer_save_exists", updated_at: save.saved_at.utc.iso8601 }, status: :conflict
            return nil
          end

          save ||= @account.app_saves.new(app_id: params[:app_id], scope: @scope, key: params[:key])
          save.assign_attributes(sha256: upload[:sha256], byte_size: upload[:bytes], saved_at: updated_at)
          save.save!
          save.install_file(upload[:file].path)
          save
        end
      end
    end
  end
end
