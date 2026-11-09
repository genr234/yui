# Fetches one imported game from its linked account (see LinkedFetch).
class LinkedFetchJob < ApplicationJob
  queue_as :default
  limits_concurrency to: 2, key: "linked_fetch", duration: 1.hour if respond_to?(:limits_concurrency)

  def perform(asset_id)
    asset = Asset.find_by(id: asset_id)
    LinkedFetch.new(asset).call if asset
  end
end
