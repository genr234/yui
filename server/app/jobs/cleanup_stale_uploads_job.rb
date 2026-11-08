# Deletes tus uploads that haven't received data for Asset::STALE_UPLOAD_AGE, with their files.
class CleanupStaleUploadsJob < ApplicationJob
  queue_as :default

  def perform
    Asset.stale_uploads.find_each(&:destroy)
  end
end
