import { expect, test } from "bun:test";
import { createFaceLock } from "./face-lock";

const player = { x: 0.42, y: 0.45, width: 0.20, height: 0.26 };
const bystander = { x: 0.74, y: 0.50, width: 0.25, height: 0.33 };

test("face lock ignores a larger bystander and bridges a short detection gap", () => {
	const track = createFaceLock();
	const leaned = { ...player, x: 0.49 };
	expect(track([player, bystander], 0)).toBeNull();
	expect(track([player], 50)).toBe(player);
	expect(track([bystander, leaned], 100)).toBe(leaned);
	expect(track([bystander], 150)).toBeNull();
	expect(track([player, bystander], 250)).toBe(player);
	expect(track([player, bystander], 1200)).toBeNull();
});
