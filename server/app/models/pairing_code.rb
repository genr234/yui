class PairingCode < ApplicationRecord
  CODE_TTL = 15.minutes

  belongs_to :account

  validates :code_digest, :expires_at, presence: true
  validates :code_digest, uniqueness: true

  scope :available, -> { where(used_at: nil).where("expires_at > ?", Time.current) }

  def self.digest_code(code)
    Digest::SHA256.hexdigest(code.to_s.gsub(/\s+/, "").upcase)
  end

  def self.create_for!(account)
    code = SecureRandom.random_number(1_000_000).to_s.rjust(6, "0")
    record = account.pairing_codes.create!(
      code_digest: digest_code(code),
      expires_at: CODE_TTL.from_now
    )
    [ record, code ]
  end

  def self.claim!(code)
    transaction do
      pairing_code = available.lock.find_by!(code_digest: digest_code(code))
      pairing_code.update!(used_at: Time.current)
      pairing_code.account
    end
  end
end
