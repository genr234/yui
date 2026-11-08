module PlaySessionTestHelper
  def create_kiosk(account: nil, name: "Front kiosk", token: SecureRandom.hex(8))
    account ||= Account.create!(name: "Lobby #{SecureRandom.hex(3)}")
    account.kiosks.create!(
      name: name,
      device_uid: "device-#{SecureRandom.hex(4)}",
      device_token_digest: Kiosk.digest_token(token)
    )
  end

  def start_session(kiosk = create_kiosk, **attributes)
    play_session, = PlaySession.find_or_start_for!(kiosk)
    play_session.update!(attributes) if attributes.any?
    play_session
  end

  def join_player(play_session, name: "Ann", color: "blue")
    Player.join!(play_session, name: name, color: color)
  end

  # Signs a cookie value the way the app's cookie jar does.
  def signed_player_cookie(player, token)
    jar = ActionDispatch::Request.new(Rails.application.env_config.deep_dup).cookie_jar
    jar.signed[PlayController::PLAYER_COOKIE] = Player.cookie_value(player, token)
    jar[PlayController::PLAYER_COOKIE]
  end
end
