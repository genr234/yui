# The asset library on the account page: the list (refreshed as a Turbo Frame while
# anything is moving) and deletion. Uploads go through AssetUploadsController.
class AssetsController < ApplicationController
  before_action :require_recent_authentication, only: :destroy
  before_action :set_account

  def index
    @assets = @account.assets.includes(:kiosk_assets).order(created_at: :desc)
    @kiosks = @account.kiosks.order(:name)
    render partial: "assets/library", locals: { account: @account, assets: @assets, kiosks: @kiosks }
  end

  def destroy
    asset = @account.assets.find(params[:id])
    asset.destroy!
    redirect_to account_path(@account, anchor: "asset-library"), notice: "Deleted #{asset.title}."
  end

  private

  def set_account
    @account = Account.find(params[:account_id])
  end
end
