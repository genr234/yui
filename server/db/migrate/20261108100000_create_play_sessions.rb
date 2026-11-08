class CreatePlaySessions < ActiveRecord::Migration[8.1]
  def change
    create_table :play_sessions do |t|
      t.references :kiosk, null: false, foreign_key: true
      t.string :code, null: false
      t.string :qr_key, null: false
      t.boolean :lobby_open, null: false, default: false
      t.datetime :ended_at

      t.timestamps
    end

    # Codes are unique among active sessions, and a kiosk has at most one active session.
    add_index :play_sessions, :code, unique: true, where: "ended_at IS NULL", name: "index_play_sessions_on_active_code"
    add_index :play_sessions, :kiosk_id, unique: true, where: "ended_at IS NULL", name: "index_play_sessions_on_active_kiosk"

    create_table :players do |t|
      t.references :play_session, null: false, foreign_key: true
      t.integer :slot
      t.string :name, null: false
      t.string :color, null: false
      t.boolean :host, null: false, default: false
      t.string :token_digest, null: false
      t.datetime :kicked_at
      t.datetime :last_seen_at
      t.boolean :connected, null: false, default: false
      t.string :connection_nonce

      t.timestamps
    end

    add_index :players, :token_digest, unique: true
    add_index :players, [ :play_session_id, :slot ], unique: true,
      where: "slot IS NOT NULL AND kicked_at IS NULL", name: "index_players_on_session_active_slot"
  end
end
