module Api
  module Kiosk
    class PairingsController < ActionController::API
      def create
        account = PairingCode.claim!(params.require(:code))
        token = ::Kiosk.issue_token
        kiosk = account.kiosks.find_or_initialize_by(device_uid: params.require(:device_uid))
        kiosk.name = params[:name].presence || "Kiosk #{kiosk.device_uid.first(8)}"
        kiosk.device_token_digest = ::Kiosk.digest_token(token)
        kiosk.connected_at ||= Time.current
        kiosk.last_seen_at = Time.current
        kiosk.save!

        render json: {
          account: account_payload(account),
          kiosk: kiosk_payload(kiosk),
          device_token: token,
          sync_cursor: account.kiosk_operations.maximum(:server_seq).to_i,
          operations: operations_payload(account.kiosk_operations.order(:server_seq))
        }
      rescue ActiveRecord::RecordNotFound
        render json: { error: "invalid or expired pairing code" }, status: :not_found
      end

      private

      def account_payload(account)
        { id: account.id.to_s, name: account.name, profile_image_url: account.profile_image_url }
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
