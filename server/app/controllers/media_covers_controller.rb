# Public cover images (phones load them too): GET /media_covers/:key
# The route only matches a 32-hex key and an image extension, and MediaCover.cached only
# returns files already in storage, so the request can't name any other path.
class MediaCoversController < ActionController::Base
  TYPES = { "png" => "image/png", "jpg" => "image/jpeg", "gif" => "image/gif", "webp" => "image/webp" }.freeze

  def show
    path = MediaCover.cached(params[:key])
    return head :not_found if path.nil?

    expires_in 1.year, public: true
    send_data path.binread, type: TYPES.fetch(path.extname.delete(".")), disposition: "inline" if stale?(last_modified: path.mtime, public: true)
  end
end
