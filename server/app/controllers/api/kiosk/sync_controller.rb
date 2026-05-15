module Api
  module Kiosk
    class SyncController < BaseController
      MAX_OPERATIONS = 250

      def push
        touch_current_kiosk!
        operations = Array(params[:operations])
        if operations.size > MAX_OPERATIONS
          return render json: { error: "too many operations", limit: MAX_OPERATIONS }, status: :payload_too_large
        end

        accepted = KioskOperation.transaction do
          operations.map do |operation|
            KioskOperation.accept!(
              account: current_account,
              kiosk: current_kiosk,
              attributes: operation_attributes(operation)
            )
          end
        end

        render json: {
          accepted: accepted.map { |operation| operation_payload(operation) },
          sync_cursor: latest_server_seq
        }
      end

      def pull
        touch_current_kiosk!
        cursor = params[:cursor].to_i
        page = current_account.kiosk_operations.where("server_seq > ?", cursor).order(:server_seq).limit(MAX_OPERATIONS + 1)
        operations = page.first(MAX_OPERATIONS)

        render json: {
          account: account_payload(current_account),
          operations: operations.map { |operation| operation_payload(operation) },
          sync_cursor: sync_cursor_for(operations, cursor),
          has_more: page.size > MAX_OPERATIONS
        }
      end

      private

      def latest_server_seq
        current_account.kiosk_operations.maximum(:server_seq).to_i
      end

      def sync_cursor_for(operations, requested_cursor)
        operations.last&.server_seq || [ requested_cursor, latest_server_seq ].min
      end

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
