# This file is auto-generated from the current state of the database. Instead
# of editing this file, please use the migrations feature of Active Record to
# incrementally modify your database, and then regenerate this schema definition.
#
# This file is the source Rails uses to define your schema when running `bin/rails
# db:schema:load`. When creating a new database, `bin/rails db:schema:load` tends to
# be faster and is potentially less error prone than running all of your
# migrations from scratch. Old migrations may fail to apply correctly if those
# migrations use external dependencies or application code.
#
# It's strongly recommended that you check this file into your version control system.

ActiveRecord::Schema[8.1].define(version: 2026_11_10_000200) do
  create_table "account_state_records", force: :cascade do |t|
    t.integer "account_id", null: false
    t.string "collection", null: false
    t.datetime "created_at", null: false
    t.boolean "deleted", default: false, null: false
    t.string "record_id", null: false
    t.integer "server_seq", null: false
    t.datetime "updated_at", null: false
    t.json "value"
    t.index ["account_id", "collection", "record_id"], name: "idx_account_state_identity", unique: true
    t.index ["account_id"], name: "index_account_state_records_on_account_id"
  end

  create_table "accounts", force: :cascade do |t|
    t.datetime "commands_cleared_at"
    t.datetime "created_at", null: false
    t.string "name", null: false
    t.datetime "operations_cleared_at"
    t.string "profile_image_url"
    t.datetime "updated_at", null: false
  end

  create_table "active_storage_attachments", force: :cascade do |t|
    t.integer "blob_id", null: false
    t.datetime "created_at", null: false
    t.string "name", null: false
    t.integer "record_id", null: false
    t.string "record_type", null: false
    t.index ["blob_id"], name: "index_active_storage_attachments_on_blob_id"
    t.index ["record_type", "record_id", "name", "blob_id"], name: "index_active_storage_attachments_uniqueness", unique: true
  end

  create_table "active_storage_blobs", force: :cascade do |t|
    t.bigint "byte_size", null: false
    t.string "checksum"
    t.string "content_type"
    t.datetime "created_at", null: false
    t.string "filename", null: false
    t.string "key", null: false
    t.text "metadata"
    t.string "service_name", null: false
    t.index ["key"], name: "index_active_storage_blobs_on_key", unique: true
  end

  create_table "active_storage_variant_records", force: :cascade do |t|
    t.integer "blob_id", null: false
    t.string "variation_digest", null: false
    t.index ["blob_id", "variation_digest"], name: "index_active_storage_variant_records_uniqueness", unique: true
    t.index ["blob_id"], name: "index_active_storage_variant_records_on_blob_id"
  end

  create_table "app_saves", force: :cascade do |t|
    t.integer "account_id", null: false
    t.string "app_id", null: false
    t.bigint "byte_size", default: 0, null: false
    t.datetime "created_at", null: false
    t.string "key", null: false
    t.datetime "saved_at", null: false
    t.string "scope", null: false
    t.string "sha256", null: false
    t.datetime "updated_at", null: false
    t.index ["account_id", "app_id", "scope", "key"], name: "index_app_saves_uniqueness", unique: true
    t.index ["account_id"], name: "index_app_saves_on_account_id"
  end

  create_table "assets", force: :cascade do |t|
    t.integer "account_id", null: false
    t.bigint "byte_size", default: 0, null: false
    t.datetime "created_at", null: false
    t.text "error"
    t.string "filename", null: false
    t.string "kind", null: false
    t.integer "linked_account_id"
    t.json "metadata", default: {}, null: false
    t.string "save_key"
    t.string "sha256"
    t.string "status", default: "uploading", null: false
    t.string "title", null: false
    t.datetime "updated_at", null: false
    t.index ["account_id", "save_key"], name: "index_assets_on_account_id_and_save_key", unique: true, where: "save_key IS NOT NULL"
    t.index ["account_id", "status"], name: "index_assets_on_account_id_and_status"
    t.index ["account_id"], name: "index_assets_on_account_id"
    t.index ["linked_account_id"], name: "index_assets_on_linked_account_id"
    t.index ["status", "updated_at"], name: "index_assets_on_status_and_updated_at"
  end

  create_table "kiosk_assets", force: :cascade do |t|
    t.integer "asset_id", null: false
    t.bigint "bytes_done", default: 0, null: false
    t.datetime "created_at", null: false
    t.text "error"
    t.integer "kiosk_id", null: false
    t.string "state", default: "queued", null: false
    t.datetime "updated_at", null: false
    t.index ["asset_id"], name: "index_kiosk_assets_on_asset_id"
    t.index ["kiosk_id", "asset_id"], name: "index_kiosk_assets_on_kiosk_id_and_asset_id", unique: true
  end

  create_table "kiosk_commands", force: :cascade do |t|
    t.string "command_type", null: false
    t.datetime "completed_at"
    t.datetime "created_at", null: false
    t.text "error"
    t.integer "kiosk_id", null: false
    t.json "payload", default: {}, null: false
    t.json "result"
    t.datetime "sent_at"
    t.string "status", default: "pending", null: false
    t.datetime "updated_at", null: false
    t.index ["kiosk_id", "status"], name: "index_kiosk_commands_on_kiosk_id_and_status"
    t.index ["kiosk_id"], name: "index_kiosk_commands_on_kiosk_id"
  end

  create_table "kiosk_events", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.json "data", default: {}, null: false
    t.string "kind", null: false
    t.integer "kiosk_id", null: false
    t.datetime "occurred_at", null: false
    t.datetime "updated_at", null: false
    t.index ["kiosk_id", "occurred_at"], name: "index_kiosk_events_on_kiosk_id_and_occurred_at"
    t.index ["kiosk_id"], name: "index_kiosk_events_on_kiosk_id"
  end

  create_table "kiosk_operations", force: :cascade do |t|
    t.integer "account_id", null: false
    t.string "action", null: false
    t.string "client_id", null: false
    t.integer "client_seq", null: false
    t.string "collection", null: false
    t.datetime "created_at", null: false
    t.integer "kiosk_id"
    t.datetime "occurred_at"
    t.json "payload"
    t.string "record_id"
    t.integer "server_seq", null: false
    t.datetime "updated_at", null: false
    t.index ["account_id", "client_id", "client_seq"], name: "idx_kiosk_operations_account_client_seq", unique: true
    t.index ["account_id", "collection"], name: "index_kiosk_operations_on_account_id_and_collection"
    t.index ["account_id", "server_seq"], name: "index_kiosk_operations_on_account_id_and_server_seq", unique: true
    t.index ["account_id"], name: "index_kiosk_operations_on_account_id"
    t.index ["kiosk_id"], name: "index_kiosk_operations_on_kiosk_id"
  end

  create_table "kiosks", force: :cascade do |t|
    t.integer "account_id", null: false
    t.datetime "connected_at"
    t.datetime "created_at", null: false
    t.string "device_token_digest", null: false
    t.string "device_uid", null: false
    t.json "health", default: {}, null: false
    t.datetime "health_at"
    t.datetime "last_seen_at"
    t.string "name", null: false
    t.datetime "updated_at", null: false
    t.index ["account_id", "device_uid"], name: "index_kiosks_on_account_id_and_device_uid", unique: true
    t.index ["account_id"], name: "index_kiosks_on_account_id"
    t.index ["device_token_digest"], name: "index_kiosks_on_device_token_digest", unique: true
  end

  create_table "linked_accounts", force: :cascade do |t|
    t.integer "account_id", null: false
    t.boolean "allow_local_network", default: false, null: false
    t.string "base_url", null: false
    t.datetime "created_at", null: false
    t.text "credentials"
    t.string "label", null: false
    t.text "last_error"
    t.datetime "last_synced_at"
    t.string "provider", null: false
    t.string "status", default: "ok", null: false
    t.datetime "syncing_since"
    t.datetime "updated_at", null: false
    t.index ["account_id"], name: "index_linked_accounts_on_account_id"
  end

  create_table "linked_imports", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.string "kind", null: false
    t.string "label", null: false
    t.integer "linked_account_id", null: false
    t.string "remote_id", null: false
    t.datetime "updated_at", null: false
    t.index ["linked_account_id", "kind", "remote_id"], name: "idx_on_linked_account_id_kind_remote_id_fd365953bc", unique: true
    t.index ["linked_account_id"], name: "index_linked_imports_on_linked_account_id"
  end

  create_table "pairing_codes", force: :cascade do |t|
    t.integer "account_id", null: false
    t.string "code_digest", null: false
    t.datetime "created_at", null: false
    t.datetime "expires_at", null: false
    t.string "purpose", default: "kiosk", null: false
    t.datetime "updated_at", null: false
    t.datetime "used_at"
    t.index ["account_id"], name: "index_pairing_codes_on_account_id"
    t.index ["code_digest"], name: "index_pairing_codes_on_code_digest", unique: true
  end

  create_table "passkey_credentials", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.datetime "last_used_at"
    t.string "nickname"
    t.text "public_key", null: false
    t.integer "sign_count", default: 0, null: false
    t.datetime "updated_at", null: false
    t.integer "user_id", null: false
    t.string "webauthn_id", null: false
    t.index ["user_id"], name: "index_passkey_credentials_on_user_id"
    t.index ["webauthn_id"], name: "index_passkey_credentials_on_webauthn_id", unique: true
  end

  create_table "phone_links", force: :cascade do |t|
    t.integer "account_id", null: false
    t.datetime "created_at", null: false
    t.string "label"
    t.datetime "last_used_at"
    t.datetime "revoked_at"
    t.string "token_digest", null: false
    t.datetime "updated_at", null: false
    t.index ["account_id"], name: "index_phone_links_on_account_id"
    t.index ["token_digest"], name: "index_phone_links_on_token_digest", unique: true
  end

  create_table "play_sessions", force: :cascade do |t|
    t.string "code", null: false
    t.datetime "created_at", null: false
    t.datetime "ended_at"
    t.integer "kiosk_id", null: false
    t.boolean "lobby_open", default: false, null: false
    t.string "qr_key", null: false
    t.datetime "updated_at", null: false
    t.index ["code"], name: "index_play_sessions_on_active_code", unique: true, where: "ended_at IS NULL"
    t.index ["kiosk_id"], name: "index_play_sessions_on_active_kiosk", unique: true, where: "ended_at IS NULL"
    t.index ["kiosk_id"], name: "index_play_sessions_on_kiosk_id"
  end

  create_table "player_profiles", force: :cascade do |t|
    t.integer "account_id", null: false
    t.string "app_id", null: false
    t.datetime "created_at", null: false
    t.text "data"
    t.datetime "updated_at", null: false
    t.integer "version", default: 0, null: false
    t.index ["account_id", "app_id"], name: "index_player_profiles_on_account_id_and_app_id", unique: true
    t.index ["account_id"], name: "index_player_profiles_on_account_id"
  end

  create_table "players", force: :cascade do |t|
    t.integer "account_id"
    t.text "avatar"
    t.string "color", null: false
    t.boolean "connected", default: false, null: false
    t.string "connection_nonce"
    t.datetime "created_at", null: false
    t.boolean "host", default: false, null: false
    t.datetime "kicked_at"
    t.datetime "last_seen_at"
    t.string "name", null: false
    t.integer "phone_link_id"
    t.integer "play_session_id", null: false
    t.integer "slot"
    t.boolean "speaker", default: false, null: false
    t.string "speaker_channel"
    t.string "token_digest", null: false
    t.datetime "updated_at", null: false
    t.index ["account_id"], name: "index_players_on_account_id"
    t.index ["phone_link_id"], name: "index_players_on_phone_link_id"
    t.index ["play_session_id", "slot"], name: "index_players_on_session_active_slot", unique: true, where: "slot IS NOT NULL AND kicked_at IS NULL"
    t.index ["play_session_id"], name: "index_players_on_play_session_id"
    t.index ["token_digest"], name: "index_players_on_token_digest", unique: true
  end

  create_table "profile_grants", force: :cascade do |t|
    t.integer "account_id", null: false
    t.string "app_id", null: false
    t.datetime "created_at", null: false
    t.datetime "updated_at", null: false
    t.index ["account_id", "app_id"], name: "index_profile_grants_on_account_id_and_app_id", unique: true
    t.index ["account_id"], name: "index_profile_grants_on_account_id"
  end

  create_table "users", force: :cascade do |t|
    t.datetime "created_at", null: false
    t.string "name", null: false
    t.datetime "updated_at", null: false
    t.string "webauthn_id", null: false
    t.index ["webauthn_id"], name: "index_users_on_webauthn_id", unique: true
  end

  add_foreign_key "account_state_records", "accounts"
  add_foreign_key "active_storage_attachments", "active_storage_blobs", column: "blob_id"
  add_foreign_key "active_storage_variant_records", "active_storage_blobs", column: "blob_id"
  add_foreign_key "app_saves", "accounts"
  add_foreign_key "assets", "accounts"
  add_foreign_key "assets", "linked_accounts"
  add_foreign_key "kiosk_assets", "assets", on_delete: :cascade
  add_foreign_key "kiosk_assets", "kiosks", on_delete: :cascade
  add_foreign_key "kiosk_commands", "kiosks"
  add_foreign_key "kiosk_events", "kiosks"
  add_foreign_key "kiosk_operations", "accounts"
  add_foreign_key "kiosk_operations", "kiosks"
  add_foreign_key "kiosks", "accounts"
  add_foreign_key "linked_accounts", "accounts"
  add_foreign_key "linked_imports", "linked_accounts"
  add_foreign_key "pairing_codes", "accounts"
  add_foreign_key "passkey_credentials", "users"
  add_foreign_key "phone_links", "accounts"
  add_foreign_key "play_sessions", "kiosks"
  add_foreign_key "player_profiles", "accounts"
  add_foreign_key "players", "accounts"
  add_foreign_key "players", "phone_links"
  add_foreign_key "players", "play_sessions"
  add_foreign_key "profile_grants", "accounts"
end
