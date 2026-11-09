# Control room: link an outside service (RomM first) to an account, test the connection,
# choose what to import, sync on demand, and unlink (plan 16). Saved credentials are never
# rendered again: forms leave the secret fields blank and a blank field keeps what is saved.
class LinkedAccountsController < ApplicationController
  TABS = %w[platforms collections search].freeze

  before_action :require_recent_authentication, only: [ :create, :update, :destroy ]
  before_action :set_account
  before_action :set_linked_account, only: [ :show, :edit, :update, :destroy, :sync, :cover, :test ]

  def new
    @linked_account = @account.linked_accounts.new(provider: "romm")
  end

  def create
    @linked_account = @account.linked_accounts.new(provider: "romm")
    assign_from_params(@linked_account)
    if @linked_account.save
      redirect_to account_linked_account_path(@account, @linked_account),
        notice: "Linked #{@linked_account.label}. Pick what to import, then sync."
    else
      render :new, status: :unprocessable_content
    end
  end

  def edit
  end

  def update
    assign_from_params(@linked_account)
    if @linked_account.save
      redirect_to account_path(@account, anchor: "linked-accounts"), notice: "Saved #{@linked_account.label}."
    else
      render :edit, status: :unprocessable_content
    end
  end

  # Checks the typed (or saved) settings without saving anything.
  def test
    candidate = @linked_account || @account.linked_accounts.new(provider: "romm")
    assign_from_params(candidate)
    result = problem = nil
    if candidate.valid?
      begin
        result = candidate.provider_client.verify!
      rescue LinkedProviders::Error => error
        problem = error.message
      end
    else
      problem = candidate.errors.full_messages.to_sentence
    end
    render partial: "linked_accounts/test_result", locals: { result: result, problem: problem }, layout: false
  end

  def show
    @tab = params[:tab].to_s.in?(TABS) ? params[:tab] : "platforms"
    @page = [ params[:page].to_i, 1 ].max
    @query = params[:q].to_s.strip
    @imported = @linked_account.linked_imports.pluck(:kind, :remote_id).to_set
    @imports = @linked_account.linked_imports.order(:kind, :label)
    load_catalog
  rescue LinkedProviders::Error => error
    @problem = error.message
  end

  def sync
    if @linked_account.syncing?
      redirect_to account_path(@account, anchor: "linked-accounts"), alert: "#{@linked_account.label} is already syncing."
    else
      LinkedAccountSyncJob.perform_later(@linked_account.id)
      redirect_to account_path(@account, anchor: "linked-accounts"), notice: "Syncing #{@linked_account.label}. Games appear in the library as they arrive."
    end
  end

  # Cover thumbnails for the import picker, fetched with the saved credentials (the
  # browser can't reach a private RomM, and must never see its login).
  def cover
    cover = @linked_account.provider_client.cover(params[:remote_id])
    return head :not_found if cover.nil?

    expires_in 1.hour, public: false
    send_data cover.bytes, type: cover.content_type, disposition: "inline"
  rescue LinkedProviders::Error, ArgumentError
    head :not_found
  end

  def destroy
    label = @linked_account.label
    if params[:assets] == "remove"
      @linked_account.assets.find_each(&:destroy)
    else
      keep_imported_games
    end
    @linked_account.destroy!
    redirect_to account_path(@account, anchor: "linked-accounts"),
      notice: "Unlinked #{label}. #{params[:assets] == "remove" ? "Its games were removed." : "Its games stay in the library."}"
  end

  private

  def set_account
    @account = Account.find(params[:account_id])
  end

  def set_linked_account
    @linked_account = @account.linked_accounts.find(params[:id]) if params[:id]
  end

  def link_params
    params.require(:linked_account).permit(:label, :base_url, :allow_local_network, :auth, :token, :username, :password)
  end

  def assign_from_params(linked_account)
    values = link_params
    linked_account.assign_attributes(
      label: values[:label], base_url: values[:base_url], allow_local_network: values[:allow_local_network] == "1"
    )
    linked_account.assign_credentials(auth: values[:auth], token: values[:token], username: values[:username], password: values[:password])
  end

  def load_catalog
    provider = @linked_account.provider_client
    case @tab
    when "platforms" then @platforms = provider.platforms
    when "collections" then @collections = provider.collections
    else @catalog = provider.catalog(page: @page, query: @query.presence) if @query.present?
    end
  end

  # Unlinking but keeping the games: they become ordinary library assets.
  def keep_imported_games
    @linked_account.assets.find_each do |asset|
      asset.update!(linked_account: nil, metadata: asset.metadata.except("source"))
    end
  end
end
