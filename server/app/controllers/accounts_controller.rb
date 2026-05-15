class AccountsController < ApplicationController
  before_action :require_recent_authentication, only: [ :create, :update, :destroy ]
  before_action :set_account, only: [ :show, :edit, :update, :destroy ]

  def index
    @accounts = Account.order(:name)
  end

  def new
    @account = Account.new
  end

  def create
    @account = Account.new(account_params)
    if @account.save
      redirect_to @account
    else
      render :new, status: :unprocessable_content
    end
  end

  def edit
  end

  def update
    @account.profile_image.purge if remove_profile_image?

    if @account.update(account_params)
      redirect_to @account, notice: "Account updated."
    else
      render :edit, status: :unprocessable_content
    end
  end

  def show
    @pairing_codes = @account.pairing_codes.order(created_at: :desc).limit(10)
    @kiosks = @account.kiosks.order(:name)
    @state_records = @account.account_state_records.order(:collection, :record_id)
    @operations = @account.kiosk_operations.order(server_seq: :desc).limit(50)
    @commands = KioskCommand.joins(:kiosk).where(kiosks: { account_id: @account.id }).order(created_at: :desc).limit(50)
  end

  def destroy
    @account.destroy
    redirect_to accounts_path, notice: "Account deleted."
  end

  private

  def set_account
    @account = Account.find(params[:id])
  end

  def account_params
    params.require(:account).permit(:name, :profile_image_url, :profile_image)
  end

  def remove_profile_image?
    params.dig(:account, :remove_profile_image) == "1" && @account.profile_image.attached?
  end
end
