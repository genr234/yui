module Api
  module Kiosk
    class PairingsController < ActionController::API
      INITIAL_OPERATIONS_LIMIT = 250
      MAX_PAIRING_ATTEMPTS = 10
      PAIRING_ATTEMPT_WINDOW = 5.minutes

      def create
        return throttled_response if pairing_attempts_exceeded?

        account = PairingCode.claim!(params.require(:code))
        reset_pairing_attempts
        token = ::Kiosk.issue_token
        kiosk = account.kiosks.find_or_initialize_by(device_uid: params.require(:device_uid))
        kiosk.name = params[:name].presence || "Kiosk #{kiosk.device_uid.first(8)}"
        kiosk.device_token_digest = ::Kiosk.digest_token(token)
        kiosk.connected_at ||= Time.current
        kiosk.last_seen_at = Time.current
        kiosk.save!
        page = account.kiosk_operations.order(:server_seq).limit(INITIAL_OPERATIONS_LIMIT + 1)
        operations = page.first(INITIAL_OPERATIONS_LIMIT)

        render json: {
          account: account_payload(account),
          kiosk: kiosk_payload(kiosk),
          device_token: token,
          sync_cursor: operations.last&.server_seq.to_i,
          has_more: page.size > INITIAL_OPERATIONS_LIMIT,
          operations: operations_payload(operations)
        }
      rescue ActiveRecord::RecordNotFound
        record_failed_pairing_attempt
        render json: { error: "invalid or expired pairing code" }, status: :not_found
      end

      private

      def pairing_attempts_exceeded?
        Rails.cache.read(pairing_attempt_cache_key).to_i >= MAX_PAIRING_ATTEMPTS
      end

      def record_failed_pairing_attempt
        attempts = Rails.cache.read(pairing_attempt_cache_key).to_i + 1
        Rails.cache.write(pairing_attempt_cache_key, attempts, expires_in: PAIRING_ATTEMPT_WINDOW)
      end

      def reset_pairing_attempts
        Rails.cache.delete(pairing_attempt_cache_key)
      end

      def throttled_response
        render json: { error: "too many pairing attempts" }, status: :too_many_requests
      end

      def pairing_attempt_cache_key
        ip = request.remote_ip.presence || "unknown"
        "kiosk_pairing_attempts:#{ip}"
      end

      def account_payload(account)
        {
          id: account.id.to_s,
          name: account.name,
          profile_image_url: account_profile_image_url(account)
        }
      end

      def account_profile_image_url(account)
        if account.profile_image.attached?
          rails_blob_url(account.profile_image, host: request.base_url)
        else
          account.profile_image_url
        end
      end

      def kiosk_payload(kiosk)
        { id: kiosk.id.to_s, name: kiosk.name, device_uid: kiosk.device_uid }
      end

      def operations_payload(operations)
        operations.map do |operation|
          {
            id: operation.id.to_s,
            server_seq: operation.server_seq,
            client_id: operation.client_id,
            client_seq: operation.client_seq,
            collection: operation.collection,
            record_id: operation.record_id,
            action: operation.action,
            payload: operation.payload
          }
        end
      end
    end
  end
end
