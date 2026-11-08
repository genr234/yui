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
  sound: { t: "sound", url: "https://example.com/a.mp3", volume: 0.8 },
  player: { t: "player", player: player({ slot: null, color: "red", host: false }) },
  system: { t: "system", action: "save_state" },
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
  system_bad_action: '{"t":"system","action":"reboot"}',
  welcome_bad_player: '{"t":"welcome","v":1,"player":{"id":7},"kiosk_name":"k"}',
  ping_no_ts: '{"t":"ping","id":1}',
  array: "[1,2]",
};
for (const [n, v] of Object.entries(valid)) await Bun.write(join(dir, n + ".json"), JSON.stringify(v) + "\n");
for (const [n, v] of Object.entries(invalid)) await Bun.write(join(dir, "invalid", n + ".json"), v + "\n");
