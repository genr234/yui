# Phone side of plan 10: link this phone to a Yui account with a one-time phone
# code, choose per session whether to play as it, and allow or revoke a game's
# profile. Same auth and CSRF rules as PlayController#join (JSON only, same
# origin, the SameSite=Lax player cookie).
module Play
  class AccountsController < PlayController
    skip_forgery_protection
    before_action :verify_same_origin_json!
    before_action :require_player

    rate_limit to: 10, within: 1.minute, only: :create,
      with: -> { render json: { error: "rate_limited" }, status: :too_many_requests }

    # POST /play/:code/account { link_code, label? }
    def create
      link, token = PhoneLink.redeem!(params.require(:link_code), label: params[:label])
      write_account_cookie(link, token)
      @player.use_account!(link)
      render_account(link)
    rescue ActiveRecord::RecordNotFound
      render json: { error: "invalid_code" }, status: :not_found
    end

    # PATCH /play/:code/account { use: bool }
    def update
      link = cookie_link
      return render json: { error: "not_linked" }, status: :conflict if ActiveModel::Type::Boolean.new.cast(params[:use]) && !link

      @player.use_account!(ActiveModel::Type::Boolean.new.cast(params[:use]) ? link : nil)
      render_account(link)
    end

    # DELETE /play/:code/account: forget the link on this phone.
    def destroy
      cookie_link&.revoke!
      @player.use_account!(nil)
      cookies.delete(account_cookie_name)
      render_account(nil)
    end

    # POST /play/:code/profile_grants { app_id }
    def grant
      account = @player.reload.profile_account
      return render json: { error: "not_linked" }, status: :conflict unless account

      account.profile_grants.find_or_create_by!(app_id: params.require(:app_id))
      head :no_content
    rescue ActiveRecord::RecordInvalid
      render json: { error: "invalid_app_id" }, status: :unprocessable_content
    rescue ActiveRecord::RecordNotUnique
      head :no_content
    end

    # DELETE /play/:code/profile_grants/:app_id
    def revoke_grant
      account = @player.reload.profile_account
      return render json: { error: "not_linked" }, status: :conflict unless account

      account.profile_grants.where(app_id: params[:app_id]).delete_all
      head :no_content
    end

    private

    def require_player
      @player = cookie_player
      render json: { error: "not_joined" }, status: :unauthorized unless @player
    end

    def render_account(link)
      render json: {
        account_name: link&.live? ? link.account.name : nil,
        account_linked: @player.reload.account_id.present?
      }
    end
  end
end
