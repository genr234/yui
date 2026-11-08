// Regenerates fixtures/input/*.json from the TS encoder. Go tests verify the
// same bytes independently, so a codec bug on either side fails a test.
import { encodeInput, inputToHex } from "../src/index";
import { join } from "node:path";

const dir = join(import.meta.dir, "..", "fixtures", "input");
const z3: [number, number, number] = [0, 0, 0];
const cases: Record<string, any> = {
  zero: { seq: 0, tMs: 0, buttons: 0, lx: 0, ly: 0, rx: 0, ry: 0, accel: z3, gyro: z3, px: 0, py: 0 },
  all_buttons: { seq: 1, tMs: 1000, buttons: 0x3ffff, lx: 0, ly: 0, rx: 0, ry: 0, accel: z3, gyro: z3, px: 0, py: 0 },
  negative_sticks: { seq: 42, tMs: 123456, buttons: 0x81, lx: -32767, ly: 32767, rx: -1, ry: -16384, accel: [0, 0, 1], gyro: z3, px: -32767, py: 32767 },
  seq_wraparound: { seq: 65535, tMs: 4294967295, buttons: 0x80000000, lx: 100, ly: -100, rx: 0, ry: 0, accel: z3, gyro: z3, px: 0, py: 0 },
  motion: { seq: 300, tMs: 50000, buttons: 5, lx: 0, ly: 0, rx: 0, ry: 0, accel: [0.5, -1.25, 9.75], gyro: [-180.5, 360, 0.125], px: -1200, py: 800 },
};
for (const [name, fields] of Object.entries(cases)) {
  const hex = inputToHex(encodeInput(fields));
  await Bun.write(join(dir, name + ".json"), JSON.stringify({ name, fields, hex }, null, 2) + "\n");
}
