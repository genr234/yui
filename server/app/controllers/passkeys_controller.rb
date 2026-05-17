class PasskeysController < ApplicationController
  skip_before_action :require_control_room_access
  before_action :require_authentication, only: [ :index, :destroy ]
  before_action :require_recent_authentication, only: :destroy
  before_action :redirect_setup_when_configured, only: :new

  def index
    @passkey_credentials = current_user.passkey_credentials.order(created_at: :asc)
  end

  def new
    @user = current_user || User.new
  end

  def options
    return head :forbidden if User.exists? && !signed_in?

    name = params[:name].to_s.strip
    name = current_user.name if name.blank? && signed_in?
    return render json: { error: "Name is required." }, status: :unprocessable_content if name.blank?

    webauthn_id = signed_in? ? current_user.webauthn_id : WebAuthn.generate_user_id
    session[:passkey_registration] = { "name" => name, "webauthn_id" => webauthn_id }

    options = relying_party.options_for_registration(
      user: { id: webauthn_id, name: name },
      exclude: signed_in? ? current_user.passkey_credentials.pluck(:webauthn_id) : [],
      authenticator_selection: { user_verification: "required" }
    )
    session[:passkey_registration_challenge] = options.challenge
    session[:passkey_rp_id] = webauthn_rp_id

    render json: options
  end

  def create
    return head :forbidden if User.exists? && !signed_in?

    registration = session[:passkey_registration]
    challenge = session[:passkey_registration_challenge]
    return redirect_to setup_path, alert: "Start passkey setup again." if registration.blank? || challenge.blank?

    webauthn_credential = relying_party.verify_registration(
      public_key_credential,
      challenge,
      user_verification: true
    )

    adding_passkey = signed_in?
    user = nil
    User.transaction do
      user = current_user || User.create!(
        name: registration.fetch("name"),
        webauthn_id: registration.fetch("webauthn_id")
      )

      user.passkey_credentials.create!(
        webauthn_id: webauthn_credential.id,
        public_key: webauthn_credential.public_key,
        sign_count: webauthn_credential.sign_count,
        nickname: params[:nickname].presence || "Passkey"
      )
    end

    session.delete(:passkey_registration)
    session.delete(:passkey_registration_challenge)
    session.delete(:passkey_rp_id)
    sign_in(user)

    redirect_to adding_passkey ? passkeys_path : accounts_path, notice: "Passkey ready."
  rescue ActiveRecord::RecordInvalid, ActiveRecord::RecordNotUnique, WebAuthn::Error, JSON::ParserError, KeyError => error
    Rails.logger.warn("Passkey setup failed: #{error.class}: #{error.message}")
    redirect_to setup_path, alert: "Passkey setup failed: #{error.message}"
  end

  def destroy
    credential = current_user.passkey_credentials.find(params[:id])

    if current_user.passkey_credentials.count <= 1
      redirect_to passkeys_path, alert: "Add another passkey before removing this one."
    else
      credential.destroy!
      redirect_to passkeys_path, notice: "Passkey removed."
    end
  end

  private

  def public_key_credential
    JSON.parse(params.require(:public_key_credential))
  end

  def redirect_setup_when_configured
    redirect_to login_path if User.exists? && !signed_in?
  end

  def webauthn_rp_id
    ENV["WEBAUTHN_RP_ID"].presence || request.host
  end

  def relying_party
    WebAuthn::RelyingParty.new(
      id: webauthn_rp_id,
      name: ENV.fetch("WEBAUTHN_RP_NAME", "Yui"),
      allowed_origins: [ request.base_url ]
    )
  end
end
