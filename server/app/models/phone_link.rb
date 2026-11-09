# A phone that may play as an account (plan 10). The phone holds the raw token
# in the signed `yui_account` cookie; only its digest is stored. Revoking a
# link ends it on that phone and in every session it is used in.
class PhoneLink < ApplicationRecord
  COOKIE_LIFETIME = 1.year
  LABEL_MAX = 40

  belongs_to :account
  has_many :players, dependent: :nullify

  scope :live, -> { where(revoked_at: nil) }

  normalizes :label, with: ->(label) { label.to_s.strip.first(LABEL_MAX).presence }

  validates :token_digest, presence: true, uniqueness: true

  # Redeems a one-time phone code. Returns [link, raw token]; raises
  # ActiveRecord::RecordNotFound for a wrong, used or expired code.
  def self.redeem!(code, label: nil)
    transaction do
      account = PairingCode.claim!(code, purpose: PairingCode::PHONE)
      token = Player.issue_token
      link = account.phone_links.create!(token_digest: Player.digest_token(token), label: label, last_used_at: Time.current)
      [ link, token ]
    end
  end

  def self.cookie_value(link, token)
    "#{link.id}:#{token}"
  end

  # The live link named by a cookie value, or nil.
  def self.from_cookie(value)
    id, token = value.to_s.split(":", 2)
    return if id.blank? || token.blank?

    link = live.find_by(id: id)
    return unless link
    return unless ActiveSupport::SecurityUtils.secure_compare(link.token_digest, Player.digest_token(token))

    link
  end

  def live?
    revoked_at.nil?
  end

  # Ends the link and drops the account from every player still using it.
  def revoke!
    return unless live?

    transaction do
      update!(revoked_at: Time.current)
      players.where.not(account_id: nil).find_each { |player| player.use_account!(nil) }
    end
  end
end
