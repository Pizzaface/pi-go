import { test } from "node:test";
import assert from "node:assert/strict";
import { Readable, Writable } from "node:stream";
import { Transport } from "../transport.js";

test("Transport.call sends JSON-RPC request and receives result", async () => {
  const out: Buffer[] = [];
  const writer = new Writable({
    write(chunk, _enc, cb) {
      out.push(chunk);
      cb();
    },
  });
  const reader = new Readable({ read() {} });
  const t = new Transport(reader, writer);

  const resultPromise = t.call("test.method", { x: 1 });

  // Wait one microtask for the write to flush.
  await new Promise((r) => setImmediate(r));
  const sent = JSON.parse(Buffer.concat(out).toString().trim());
  assert.equal(sent.method, "test.method");
  assert.deepEqual(sent.params, { x: 1 });
  assert.equal(typeof sent.id, "number");

  reader.push(JSON.stringify({ jsonrpc: "2.0", id: sent.id, result: { ok: true } }) + "\n");

  const result = await resultPromise;
  assert.deepEqual(result, { ok: true });
  t.close();
});

test("Transport.notify sends request without id", async () => {
  const out: Buffer[] = [];
  const writer = new Writable({
    write(chunk, _enc, cb) {
      out.push(chunk);
      cb();
    },
  });
  const reader = new Readable({ read() {} });
  const t = new Transport(reader, writer);

  t.notify("test.log", { message: "hi" });
  await new Promise((r) => setImmediate(r));
  const sent = JSON.parse(Buffer.concat(out).toString().trim());
  assert.equal(sent.method, "test.log");
  assert.equal(sent.id, undefined);
  t.close();
});
