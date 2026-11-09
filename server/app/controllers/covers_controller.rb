# Public game covers (phones load them too): GET /covers/:game_id.png
# The route only matches [A-Z0-9]{6}, and GameCover.cached only returns files that
# already exist in storage/covers, so the request can't name any other path. Covers
# are small (GameCover::MAX_BYTES), so they're sent from memory.
class CoversController < ActionController::Base
  def show
    path = GameCover.cached(params[:game_id])
    return head :not_found if path.nil?

    expires_in 1.day, public: true
    send_data path.binread, type: "image/png", disposition: "inline" if stale?(last_modified: path.mtime, public: true)
  end

  # Covers of games imported from a linked account: GET /covers/linked/:key
  def linked
    path = LinkedCover.cached(params[:key])
    return head :not_found if path.nil?

    expires_in 1.day, public: true
    send_data path.binread, type: LinkedCover.content_type(path.basename.to_s), disposition: "inline" if stale?(last_modified: path.mtime, public: true)
  end
end
