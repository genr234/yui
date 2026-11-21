import { describe, expect, test } from "bun:test";
import { readdirSync, readFileSync, existsSync } from "node:fs";
import { join } from "node:path";
import { stringArrayField, stringField } from "./source-fields";

const appsDir = join(import.meta.dir, "../../../../apps");

describe("source fields", () => {
	test("ignores keys that only end in the field name", () => {
		const source = `const t = { titleId: meta.title_id : "", padId: "p1" };
export default { id: "dev.example.real" };`;
		expect(stringField(source, "id")).toBe("dev.example.real");
	});

	test("ignores property access", () => {
		expect(stringField(`x ? meta.id : "nope"; export default { id: "a.b.c" }`, "id")).toBe("a.b.c");
	});

	test("reads arrays only from a real key", () => {
		expect(stringArrayField(`extraPermissions: ["x"], permissions: ["camera"]`, "permissions")).toEqual(["camera"]);
	});

	for (const dir of readdirSync(appsDir)) {
		const entry = join(appsDir, dir, "app.yui.js");
		if (!existsSync(entry)) continue;
		test(`apps/${dir} reads its own id`, () => {
			expect(stringField(readFileSync(entry, "utf8"), "id")).toMatch(/^dev\.genr\./);
		});
	}
});
