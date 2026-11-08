# Be sure to restart your server when you modify this file.

# Define an application-wide content security policy.
# See the Securing Rails Applications Guide for more information:
# https://guides.rubyonrails.org/security.html#content-security-policy-header

Rails.application.configure do
  config.content_security_policy do |policy|
    policy.default_src :self
    policy.base_uri    :self
    policy.connect_src :self, :https, :wss
    policy.font_src    :self, :data
    policy.form_action :self
    policy.frame_ancestors :none
    policy.img_src     :self, :https, :data
    policy.object_src  :none
    policy.script_src  :self
    policy.style_src   :self
  end

  # Fresh random nonce per request. Deriving it from the session id leaves it
  # empty ('nonce-') on pages served before a session exists, such as /setup,
  # which blocks the importmap scripts. Rails memoizes the nonce per request,
  # so the header, csp_meta_tag and javascript_importmap_tags all agree, and
  # Turbo ignores nonces when it diffs <head> scripts between visits.
  config.content_security_policy_nonce_generator = ->(_request) { SecureRandom.base64(16) }
  config.content_security_policy_nonce_directives = %w[script-src style-src]
  config.content_security_policy_nonce_auto = true
end
