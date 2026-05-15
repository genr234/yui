module ApplicationHelper
  def account_profile_image_url(account)
    if account.profile_image.attached?
      url_for(account.profile_image)
    elsif account.profile_image_url.present?
      account.profile_image_url
    end
  end
end
