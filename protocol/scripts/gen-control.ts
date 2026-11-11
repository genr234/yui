// One-off helper that writes fixtures/control/**. Edit fixtures by hand afterwards if needed.
import { join } from "node:path";

const dir = join(import.meta.dir, "..", "fixtures", "control");
const player = (o: object = {}) => ({ id: "7", slot: 1, name: "Ann", color: "blue", host: true, connected: true, ...o });

const valid: Record<string, unknown> = {
  hello: { t: "hello", v: 1, caps: { motion: true, haptics: false, wakeLock: true }, screen: { w: 390, h: 844, orientation: "portrait" } },
  welcome: { t: "welcome", v: 1, player: player(), kiosk_name: "Lobby kiosk" },
  layout: { t: "layout", version: 3, tree: { type: "column", children: [{ type: "text", props: { text: "Hi" } }, { type: "button", props: { id: "a", label: "A" } }] } },
  event: { t: "event", control: "a", kind: "press" },
  event_value: { t: "event", control: "vol", kind: "change", value: 0.5 },
  text: { t: "text", op: "insert", text: "x" },
  haptic: { t: "haptic", pattern: [30, 20, 30] },
  haptic_named: { t: "haptic", name: "success", intensity: 0.5 },
  sound: { t: "sound", url: "https://example.com/a.mp3", volume: 0.8 },
  player: { t: "player", player: player({ slot: null, color: "red", host: false }) },
  system: { t: "system", action: "save_state" },
  react: { t: "react", emote: "heart" },
  poke: { t: "poke", to: "8" },
  poked: { t: "poked", from: "7" },
  players: { t: "players", players: [{ id: "8", name: "Bo", color: "red", slot: 2, avatar: null, pokes: true }, { id: "9", name: "Cy", color: "green", slot: null, pokes: false }] },
  system_pokes: { t: "system", action: "pokes", value: false },
  seat_offer: { t: "seat_offer", to: "8" },
  seat_offered: { t: "seat_offered", from: { id: "7", name: "Ana", color: "blue" }, slot: 1, expires_in: 20 },
  seat_answer: { t: "seat_answer", from: "7", accept: true },
  seat_cancel: { t: "seat_cancel" },
  seat_result: { t: "seat_result", result: "accepted", role: "giver", slot: 1, other: { id: "8", name: "Dee", color: "red" } },
  ping: { t: "ping", id: 1, ts: 1700000000000 },
  pong: { t: "pong", id: 1, ts: 1700000000001 },
};
const invalid: Record<string, string> = {
  not_json: "nope{",
  missing_t: '{"v":1}',
  unknown_t: '{"t":"bogus"}',
  event_no_control: '{"t":"event","kind":"press"}',
  event_bad_kind: '{"t":"event","control":"a","kind":"hold"}',
  layout_no_tree: '{"t":"layout","version":1}',
  layout_bad_node: '{"t":"layout","version":1,"tree":{"props":{}}}',
  hello_no_caps: '{"t":"hello","v":1,"screen":{"w":1,"h":1,"orientation":"portrait"}}',
  haptic_bad: '{"t":"haptic","pattern":"30"}',
  haptic_both: '{"t":"haptic","name":"tap","pattern":[10]}',
  haptic_unknown_name: '{"t":"haptic","name":"zap"}',
  haptic_too_long: '{"t":"haptic","pattern":[1000,1000,1000,1000,1000]}',
  haptic_bad_intensity: '{"t":"haptic","name":"tap","intensity":2}',
  system_bad_action: '{"t":"system","action":"reboot"}',
  welcome_bad_player: '{"t":"welcome","v":1,"player":{"id":7},"kiosk_name":"k"}',
  react_unknown_emote: '{"t":"react","emote":"poop"}',
  poke_no_to: '{"t":"poke"}',
  poked_no_from: '{"t":"poked"}',
  players_bad: '{"t":"players","players":[{"id":1}]}',
  seat_offer_no_to: '{"t":"seat_offer"}',
  seat_answer_no_accept: '{"t":"seat_answer","from":"7"}',
  seat_offered_bad_slot: '{"t":"seat_offered","from":{"id":"7","name":"Ana","color":"blue"},"slot":0,"expires_in":20}',
  seat_result_bad_result: '{"t":"seat_result","result":"maybe","role":"giver","slot":1}',
  ping_no_ts: '{"t":"ping","id":1}',
  array: "[1,2]",
};
for (const [n, v] of Object.entries(valid)) await Bun.write(join(dir, n + ".json"), JSON.stringify(v) + "\n");
for (const [n, v] of Object.entries(invalid)) await Bun.write(join(dir, "invalid", n + ".json"), v + "\n");
