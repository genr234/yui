# Control room: add or remove one import (platform, collection or single game) on a
# linked account. Nothing downloads until a sync runs; games that leave every import are
# removed by the next sync.
class LinkedImportsController < ApplicationController
  before_action :require_recent_authentication
  before_action :set_linked_account

  def create
    import = @linked_account.linked_imports.find_or_initialize_by(kind: params[:kind].to_s, remote_id: params[:remote_id].to_s)
    import.label = params[:label].to_s.strip.truncate(200)
    if import.save
      redirect_to picker_path, notice: "Added #{import.label}. Sync to bring it in."
    else
      redirect_to picker_path, alert: import.errors.full_messages.to_sentence
    end
  end

  def destroy
    import = @linked_account.linked_imports.find(params[:id])
    import.destroy!
    redirect_to picker_path, notice: "Removed #{import.label}. Its games leave the library at the next sync."
  end

  private

  def set_linked_account
    @account = Account.find(params[:account_id])
    @linked_account = @account.linked_accounts.find(params[:linked_account_id])
  end

  def picker_path
    account_linked_account_path(@account, @linked_account, params.permit(:tab, :q, :page).to_h.symbolize_keys)
  end
end
