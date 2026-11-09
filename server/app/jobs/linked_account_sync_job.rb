# Syncs one linked account's imports into its asset library (see LinkedSync). Queued by
# "Sync now" and by LinkedAccountsSyncJob.
class LinkedAccountSyncJob < ApplicationJob
  queue_as :default

  def perform(linked_account_id)
    linked_account = LinkedAccount.find_by(id: linked_account_id)
    LinkedSync.new(linked_account).call if linked_account
  end
end
