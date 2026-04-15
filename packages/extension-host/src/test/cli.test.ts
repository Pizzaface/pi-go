import { test } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";

const __dirname = dirname(fileURLToPath(import.meta.url));
const cli = resolve(__dirname, "../cli.js");

test("CLI exits 2 when --entry missing", () => {
  const res = spawnSync("node", [cli], { encoding: "utf8" });
  assert.equal(res.status, 2);
  assert.match(res.stderr, /--entry is required/);
});
