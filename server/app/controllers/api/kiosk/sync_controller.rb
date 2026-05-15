module Api
  module Kiosk
    class SyncController < BaseController
      def push
        touch_current_kiosk!
        accepted = Array(params[:operations]).map do |operation|
          KioskOperation.accept!(
            account: current_account,
            kiosk: current_kiosk,
            attributes: operation_attributes(operation)
          )
        end

        render json: {
          accepted: accepted.map { |operation| operation_payload(operation) },
          sync_cursor: current_account.kiosk_operations.maximum(:server_seq).to_i
        }
      end

      def pull
        touch_current_kiosk!
        cursor = params[:cursor].to_i
        operations = current_account.kiosk_operations.where("server_seq > ?", cursor).order(:server_seq)

        render json: {
          account: account_payload(current_account),
          operations: operations.map { |operation| operation_payload(operation) },
          sync_cursor: current_account.kiosk_operations.maximum(:server_seq).to_i
        }
      end

      private

      def operation_attributes(operation)
        if operation.respond_to?(:to_unsafe_h)
          operation.to_unsafe_h.symbolize_keys
        else
          operation.to_h.symbolize_keys
        end
      end

      def operation_payload(operation)
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
    end
  end
end
