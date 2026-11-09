# Recurring (every 6 hours): queues a sync for every linked account that has imports.
class LinkedAccountsSyncJob < ApplicationJob
  queue_as :default

  def perform
    LinkedAccount.syncable.where(id: LinkedImport.select(:linked_account_id)).find_each do |linked_account|
      LinkedAccountSyncJob.perform_later(linked_account.id)
    end
  end
end
