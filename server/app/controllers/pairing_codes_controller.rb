class PairingCodesController < ApplicationController
  before_action :require_recent_authentication

  def create
    account = Account.find(params[:account_id])
    pairing_code, plain_code = PairingCode.create_for!(account)

    redirect_to account_path(account), notice: "Pairing code: #{plain_code}. Expires at #{pairing_code.expires_at}."
  end
end
