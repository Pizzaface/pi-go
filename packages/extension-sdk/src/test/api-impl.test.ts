import { test } from "node:test";
import assert from "node:assert/strict";
import { Readable, Writable } from "node:stream";
import { Transport } from "../transport.js";
import { createExtensionAPI } from "../api-impl.js";
import { NotImplementedError, CapabilityDeniedError } from "../errors.js";

function pair(): { reader: Readable; writer: Writable; written: Buffer[] } {
  const written: Buffer[] = [];
  return {
    reader: new Readable({ read() {} }),
    writer: new Writable({ write(c, _e, cb) { written.push(c); cb(); } }),
    written,
  };
}

test("registerTool with grant sends host_call", async () => {
  const { reader, writer, written } = pair();
  const t = new Transport(reader, writer);
  const api = createExtensionAPI(t, { name: "x", version: "0.1", requestedCapabilities: [] },
    [{ service: "tools", version: 1, methods: ["register"] }]);
  api.registerTool({
    name: "greet", label: "g", description: "g", parameters: {}, execute: async () => ({ content: [] }),
  });
  await new Promise((r) => setImmediate(r));
  const sent = JSON.parse(Buffer.concat(written).toString().trim());
  assert.equal(sent.method, "pi.extension/host_call");
  assert.equal(sent.params.service, "tools");
  assert.equal(sent.params.method, "register");
  // Settle the pending host_call so close() doesn't trigger an unhandled rejection.
  reader.push(JSON.stringify({ jsonrpc: "2.0", id: sent.id, result: null }) + "\n");
  await new Promise((r) => setImmediate(r));
  t.close();
});

test("registerTool without grant throws CapabilityDenied", () => {
  const { reader, writer } = pair();
  const t = new Transport(reader, writer);
  const api = createExtensionAPI(t, { name: "x", version: "0.1", requestedCapabilities: [] }, []);
  assert.throws(() => api.registerTool({
    name: "greet", label: "g", description: "g", parameters: {}, execute: async () => ({ content: [] }),
  }), CapabilityDeniedError);
  t.close();
});

test("registerCommand throws NotImplementedError", () => {
  const { reader, writer } = pair();
  const t = new Transport(reader, writer);
  const api = createExtensionAPI(t, { name: "x", version: "0.1", requestedCapabilities: [] }, []);
  assert.throws(() => api.registerCommand("x", { description: "d", handler: async () => {} }), NotImplementedError);
  t.close();
});
