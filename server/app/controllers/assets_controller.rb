# The asset library on the account page: the list (refreshed as a Turbo Frame while
# anything is moving) and deletion. Uploads go through AssetUploadsController.
class AssetsController < ApplicationController
  before_action :require_recent_authentication, only: %i[update destroy]
  before_action :set_account

  def index
    @assets = @account.assets.includes(:kiosk_assets).order(created_at: :desc)
    @kiosks = @account.kiosks.order(:name)
    render partial: "assets/library", locals: { account: @account, assets: @assets, kiosks: @kiosks }
  end

  # Attaches an image (media asset) to a game as its cover, or detaches it (blank cover_for).
  def update
    image = @account.assets.find(params[:id])
    game = @account.assets.where(kind: "game").find_by(id: params[:cover_for].presence) if params[:cover_for].present?
    if params[:cover_for].present? && (game.nil? || !image.image? || !image.ready? || (key = MediaCover.store_file(image.file_path)).nil?)
      return redirect_to account_path(@account, anchor: "asset-library"), alert: "Pick a game, and an image of up to 5 MB."
    end

    metadata = image.metadata.to_h.except("cover_for", "cover_key")
    metadata = metadata.merge("cover_for" => game.id.to_s, "cover_key" => key) if game
    image.update!(metadata: metadata)
    image.request_kiosk_reconcile!
    redirect_to account_path(@account, anchor: "asset-library"), notice: game ? "#{image.title} is now the cover for #{game.title}." : "Removed the cover from #{image.title}."
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
