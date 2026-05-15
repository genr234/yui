class SessionsController < ApplicationController
  skip_before_action :require_control_room_access

  def new
    redirect_to setup_path unless User.exists?
  end

  def options
    credentials = PasskeyCredential.pluck(:webauthn_id)
    return render json: { error: "No passkeys are registered." }, status: :not_found if credentials.empty?

    options = relying_party.options_for_authentication(
      allow: credentials,
      user_verification: "required"
    )
    session[:passkey_authentication_challenge] = options.challenge
    session[:passkey_rp_id] = webauthn_rp_id

    render json: options
  end

  def create
    challenge = session[:passkey_authentication_challenge]
    return redirect_to login_path, alert: "Start passkey login again." if challenge.blank?

    webauthn_credential, credential = relying_party.verify_authentication(
      public_key_credential,
      challenge,
      user_verification: true
    ) do |candidate|
      PasskeyCredential.find_by!(webauthn_id: candidate.id)
    end

    credential.update!(
      sign_count: webauthn_credential.sign_count,
      last_used_at: Time.current
    )
    session.delete(:passkey_authentication_challenge)
    session.delete(:passkey_rp_id)
    sign_in(credential.user)

    redirect_to after_authentication_path, notice: "Logged in successfully."
  rescue ActiveRecord::RecordNotFound, WebAuthn::Error, JSON::ParserError => error
    Rails.logger.warn("Passkey login failed: #{error.class}: #{error.message}")
    redirect_to login_path, alert: "Passkey login failed: #{error.message}"
  end

  def destroy
    sign_out
    redirect_to login_path, notice: "Signed out."
  end

  private

  def public_key_credential
    JSON.parse(params.require(:public_key_credential))
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
