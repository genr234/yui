module Api
  module Kiosk
    # A one-time code a phone redeems to play as this kiosk's account (plan 10).
    # The overlay asks for it from PIN-protected Settings.
    class PhoneLinkCodesController < BaseController
      def create
        record, code = PairingCode.create_for!(current_account, purpose: PairingCode::PHONE)
        render json: { code: code, expires_at: record.expires_at.iso8601 }, status: :created
      end
    end
  end
end
