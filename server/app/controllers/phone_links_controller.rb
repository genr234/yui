# Control room: make a one-time code that links a phone to play as this
# account (plan 10), and revoke linked phones.
class PhoneLinksController < ApplicationController
  before_action :require_recent_authentication
  before_action :set_account

  def create
    record, code = PairingCode.create_for!(@account, purpose: PairingCode::PHONE)
    redirect_to account_path(@account), notice: "Phone code: #{code}. Enter it on the phone under \"Use my Yui account\". Expires at #{record.expires_at}."
  end

  def destroy
    @account.phone_links.find(params[:id]).revoke!
    redirect_to account_path(@account), notice: "That phone no longer plays as #{@account.name}."
  end

  private

  def set_account
    @account = Account.find(params[:account_id])
  end
end
