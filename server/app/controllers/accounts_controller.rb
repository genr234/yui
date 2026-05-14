class AccountsController < ApplicationController
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
    @account = Account.find(params[:id])
  end

  def update
    @account = Account.find(params[:id])
    if @account.update(account_params)
      redirect_to @account, notice: "Account updated."
    else
      render :edit, status: :unprocessable_content
    end
  end

  def show
    @account = Account.find(params[:id])
    @pairing_codes = @account.pairing_codes.order(created_at: :desc).limit(10)
    @kiosks = @account.kiosks.order(:name)
    @state_records = @account.account_state_records.order(:collection, :record_id)
    @operations = @account.kiosk_operations.order(server_seq: :desc).limit(50)
    @commands = KioskCommand.joins(:kiosk).where(kiosks: { account_id: @account.id }).order(created_at: :desc).limit(50)
  end

  private

  def account_params
    params.require(:account).permit(:name, :profile_image_url)
  end
end
