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

ActiveRecord::Schema[8.1].define(version: 2026_05_15_103000) do
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
    t.datetime "created_at", null: false
    t.string "name", null: false
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
    t.datetime "last_seen_at"
    t.string "name", null: false
    t.datetime "updated_at", null: false
    t.index ["account_id", "device_uid"], name: "index_kiosks_on_account_id_and_device_uid", unique: true
    t.index ["account_id"], name: "index_kiosks_on_account_id"
    t.index ["device_token_digest"], name: "index_kiosks_on_device_token_digest", unique: true
  end

  create_table "pairing_codes", force: :cascade do |t|
    t.integer "account_id", null: false
    t.string "code_digest", null: false
    t.datetime "created_at", null: false
    t.datetime "expires_at", null: false
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
  add_foreign_key "kiosk_commands", "kiosks"
  add_foreign_key "kiosk_operations", "accounts"
  add_foreign_key "kiosk_operations", "kiosks"
  add_foreign_key "kiosks", "accounts"
  add_foreign_key "pairing_codes", "accounts"
  add_foreign_key "passkey_credentials", "users"
end
