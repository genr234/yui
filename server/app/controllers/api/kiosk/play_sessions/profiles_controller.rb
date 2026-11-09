module Api
  module Kiosk
    module PlaySessions
      # A game's profile for a player in this kiosk's active session (plan 10).
      # The player must be playing as an account, and that account must have
      # allowed this app id from the phone (ProfileGrant).
      class ProfilesController < BaseController
        before_action :set_profile_account

        # GET …/players/:player_id/profiles/:app_id
        def show
          profile = PlayerProfile.read(@account, params[:app_id])
          render json: profile_json(profile)
        end

        # PUT …/players/:player_id/profiles/:app_id { data, version }
        def update
          body = request.request_parameters
          return render json: { error: "data_required" }, status: :unprocessable_content unless body.key?("data")

          version = Integer(body.fetch("version", 0))
          profile = PlayerProfile.write!(@account, params[:app_id], body["data"], version)
          render json: profile_json(profile).except(:data)
        rescue PlayerProfile::VersionConflict => error
          render json: { error: "version_conflict", version: error.current.version }, status: :conflict
        rescue PlayerProfile::TooLarge
          render json: { error: "too_large", max_bytes: PlayerProfile::MAX_BYTES }, status: :content_too_large
        rescue ArgumentError, TypeError
          render json: { error: "invalid_version" }, status: :unprocessable_content
        end

        private

        def set_profile_account
          play_session = current_kiosk.play_sessions.active.find_by(id: params[:play_session_id])
          player = play_session&.players&.active&.find_by(id: params[:player_id])
          return render json: { error: "not_found" }, status: :not_found unless player
          unless params[:app_id].to_s.match?(ProfileGrant::APP_ID_FORMAT)
            return render json: { error: "invalid_app_id" }, status: :unprocessable_content
          end

          @account = player.profile_account
          return render json: { error: "not_linked" }, status: :conflict unless @account
          return if @account.profile_grants.exists?(app_id: params[:app_id])

          render json: { error: "not_granted" }, status: :forbidden
        end

        def profile_json(profile)
          {
            profile_id: PlayerProfile.profile_id(@account, profile.app_id),
            data: profile.parsed_data,
            version: profile.version,
            updated_at: profile.updated_at&.iso8601
          }
        end
      end
    end
  end
end
