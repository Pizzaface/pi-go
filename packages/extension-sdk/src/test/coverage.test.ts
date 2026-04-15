import { test } from "node:test";
import assert from "node:assert/strict";
import { Readable, Writable } from "node:stream";
import { Transport } from "../transport.js";
import { createExtensionAPI } from "../api-impl.js";
import { EventNames } from "../events.js";
import { NotImplementedError, CapabilityDeniedError } from "../errors.js";

function pair(): { reader: Readable; writer: Writable; written: Buffer[] } {
  const written: Buffer[] = [];
  return {
    reader: new Readable({ read() {} }),
    writer: new Writable({ write(c, _e, cb) { written.push(c); cb(); } }),
    written,
  };
}

function parseLines(written: Buffer[]): Record<string, unknown>[] {
  return Buffer.concat(written)
    .toString()
    .split("\n")
    .filter((l) => l.trim().length > 0)
    .map((l) => JSON.parse(l) as Record<string, unknown>);
}

test("Transport ignores malformed input lines", async () => {
  const { reader, writer } = pair();
  const t = new Transport(reader, writer);
  reader.push("not-json\n");
  reader.push("\n"); // blank line
  await new Promise((r) => setImmediate(r));
  t.close();
});

test("Transport.call rejects when close happens with pending request", async () => {
  const { reader, writer } = pair();
  const t = new Transport(reader, writer);
  const p = t.call("hang", {});
  t.close();
  await assert.rejects(p, /transport closed/);
});

test("Transport.call after close rejects immediately", async () => {
  const { reader, writer } = pair();
  const t = new Transport(reader, writer);
  t.close();
  await assert.rejects(t.call("x", {}), /transport closed/);
});

test("Transport.notify after close is a no-op", () => {
  const { reader, writer, written } = pair();
  const t = new Transport(reader, writer);
  t.close();
  t.notify("x", {});
  assert.equal(written.length, 0);
});

test("Transport delivers rpc error response to caller", async () => {
  const { reader, writer, written } = pair();
  const t = new Transport(reader, writer);
  const p = t.call("boom", {});
  await new Promise((r) => setImmediate(r));
  const sent = JSON.parse(Buffer.concat(written).toString().trim()) as { id: number };
  reader.push(JSON.stringify({ jsonrpc: "2.0", id: sent.id, error: { code: 42, message: "nope" } }) + "\n");
  await assert.rejects(p, /rpc 42: nope/);
  t.close();
});

test("Transport dispatches incoming request to handler and writes response", async () => {
  const { reader, writer, written } = pair();
  const t = new Transport(reader, writer);
  t.handle("ping", async (params) => {
    const p = params as { n: number };
    return { pong: p.n + 1 };
  });
  reader.push(JSON.stringify({ jsonrpc: "2.0", id: 7, method: "ping", params: { n: 4 } }) + "\n");
  // wait two ticks for async handler + write
  await new Promise((r) => setImmediate(r));
  await new Promise((r) => setImmediate(r));
  const sent = parseLines(written);
  assert.equal(sent.length, 1);
  assert.equal(sent[0].id, 7);
  assert.deepEqual(sent[0].result, { pong: 5 });
  t.close();
});

test("Transport handler that throws sends error response", async () => {
  const { reader, writer, written } = pair();
  const t = new Transport(reader, writer);
  t.handle("kaboom", async () => { throw new Error("handler failed"); });
  reader.push(JSON.stringify({ jsonrpc: "2.0", id: 11, method: "kaboom" }) + "\n");
  await new Promise((r) => setImmediate(r));
  await new Promise((r) => setImmediate(r));
  const sent = parseLines(written);
  assert.equal(sent.length, 1);
  const msg = sent[0] as { id: number; error: { code: number; message: string } };
  assert.equal(msg.id, 11);
  assert.equal(msg.error.code, -32603);
  assert.match(msg.error.message, /handler failed/);
  t.close();
});

test("Transport dispatches incoming notification to handler", async () => {
  const { reader, writer, written } = pair();
  const t = new Transport(reader, writer);
  let seen: unknown;
  t.handle("log", async (params) => { seen = params; });
  reader.push(JSON.stringify({ jsonrpc: "2.0", method: "log", params: { msg: "hi" } }) + "\n");
  await new Promise((r) => setImmediate(r));
  assert.deepEqual(seen, { msg: "hi" });
  // No response should be written for a notification.
  assert.equal(written.length, 0);
  t.close();
});

test("Transport ignores result for unknown pending id", async () => {
  const { reader, writer } = pair();
  const t = new Transport(reader, writer);
  reader.push(JSON.stringify({ jsonrpc: "2.0", id: 999, result: null }) + "\n");
  await new Promise((r) => setImmediate(r));
  t.close();
});

test("api.name/version and unimplemented methods", () => {
  const { reader, writer } = pair();
  const t = new Transport(reader, writer);
  const api = createExtensionAPI(t, { name: "ext", version: "1.2.3", requestedCapabilities: [] }, []);
  assert.equal(api.name(), "ext");
  assert.equal(api.version(), "1.2.3");
  assert.throws(() => api.registerShortcut("s", { description: "d", handler: () => {} }), NotImplementedError);
  assert.throws(() => api.registerFlag("f", { description: "d", type: "boolean" }), NotImplementedError);
  assert.throws(() => api.registerProvider("p", {}), NotImplementedError);
  assert.throws(() => api.unregisterProvider("p"), NotImplementedError);
  assert.throws(() => api.registerMessageRenderer("t", { kind: "text", handler: () => ({}) }), NotImplementedError);
  assert.throws(() => api.events.on("e", () => {}), NotImplementedError);
  assert.throws(() => api.events.emit("e", {}), NotImplementedError);
  assert.throws(() => api.sendMessage({ customType: "c", content: "" }), NotImplementedError);
  assert.throws(() => api.sendUserMessage("hi"), NotImplementedError);
  assert.throws(() => api.appendEntry("c"), NotImplementedError);
  assert.throws(() => api.setSessionName("n"), NotImplementedError);
  assert.throws(() => api.setLabel("e", undefined), NotImplementedError);
  assert.throws(() => api.setActiveTools([]), NotImplementedError);
  assert.throws(() => api.setThinkingLevel("low"), NotImplementedError);
  assert.equal(api.getSessionName(), undefined);
  assert.deepEqual(api.getActiveTools(), []);
  assert.deepEqual(api.getAllTools(), []);
  assert.equal(api.getThinkingLevel(), "off");
  assert.deepEqual(api.getCommands(), []);
  assert.equal(api.getFlag("x"), undefined);
  t.close();
});

test("api.setModel rejects with NotImplementedError", async () => {
  const { reader, writer } = pair();
  const t = new Transport(reader, writer);
  const api = createExtensionAPI(t, { name: "ext", version: "0", requestedCapabilities: [] }, []);
  await assert.rejects(api.setModel({ provider: "x", id: "y" }), NotImplementedError);
  t.close();
});

test("api.registerTool rejects missing name or execute", () => {
  const { reader, writer } = pair();
  const t = new Transport(reader, writer);
  const api = createExtensionAPI(t, { name: "ext", version: "0", requestedCapabilities: [] },
    [{ service: "tools", version: 1, methods: ["register"] }]);
  assert.throws(() => api.registerTool({
    name: "", label: "l", description: "d", parameters: {}, execute: async () => ({ content: [] }),
  }), /name and execute are required/);
  t.close();
});

test("api.on(session_start) subscribes and dispatches events", async () => {
  const { reader, writer, written } = pair();
  const t = new Transport(reader, writer);
  const api = createExtensionAPI(t, { name: "ext", version: "0", requestedCapabilities: [] },
    [{ service: "events", version: 1, methods: ["session_start"] }]);
  const seen: unknown[] = [];
  api.on(EventNames.SessionStart, async (evt) => {
    seen.push(evt);
    return { control: { block: true, reason: "stop" } };
  });
  // The subscribe call is in-flight; settle it.
  await new Promise((r) => setImmediate(r));
  const subReq = parseLines(written)[0] as { id: number; method: string };
  assert.equal(subReq.method, "pi.extension/subscribe_event");
  reader.push(JSON.stringify({ jsonrpc: "2.0", id: subReq.id, result: null }) + "\n");
  // Deliver a session_start event.
  reader.push(JSON.stringify({
    jsonrpc: "2.0", id: 100, method: "pi.extension/extension_event",
    params: { event: EventNames.SessionStart, payload: { reason: "startup" } },
  }) + "\n");
  await new Promise((r) => setImmediate(r));
  await new Promise((r) => setImmediate(r));
  assert.deepEqual(seen, [{ reason: "startup" }]);
  const sent = parseLines(written);
  const evtResponse = sent.find((m) => m.id === 100) as { result: { control: { block: boolean; reason: string } } };
  assert.deepEqual(evtResponse.result, { control: { block: true, reason: "stop" } });
  t.close();
});

test("api.on throws for non-session_start events", () => {
  const { reader, writer } = pair();
  const t = new Transport(reader, writer);
  const api = createExtensionAPI(t, { name: "ext", version: "0", requestedCapabilities: [] }, []);
  assert.throws(() => api.on("tool_execute" as never, () => {}), NotImplementedError);
  t.close();
});

test("api.on without grant throws CapabilityDenied", () => {
  const { reader, writer } = pair();
  const t = new Transport(reader, writer);
  const api = createExtensionAPI(t, { name: "ext", version: "0", requestedCapabilities: [] }, []);
  assert.throws(() => api.on(EventNames.SessionStart, () => {}), CapabilityDeniedError);
  t.close();
});

test("api.exec forwards to host_call and returns result", async () => {
  const { reader, writer, written } = pair();
  const t = new Transport(reader, writer);
  const api = createExtensionAPI(t, { name: "ext", version: "0", requestedCapabilities: [] },
    [{ service: "exec", version: 1, methods: ["shell"] }]);
  const p = api.exec("echo", ["hi"], { timeout: 1000 });
  await new Promise((r) => setImmediate(r));
  const req = parseLines(written)[0] as {
    id: number; method: string; params: { service: string; method: string; payload: { cmd: string; args: string[] } };
  };
  assert.equal(req.method, "pi.extension/host_call");
  assert.equal(req.params.service, "exec");
  assert.equal(req.params.method, "shell");
  assert.equal(req.params.payload.cmd, "echo");
  reader.push(JSON.stringify({
    jsonrpc: "2.0", id: req.id, result: { stdout: "hi\n", stderr: "", code: 0, killed: false },
  }) + "\n");
  const result = await p;
  assert.equal(result.stdout, "hi\n");
  assert.equal(result.code, 0);
  t.close();
});

test("api.exec without grant throws CapabilityDenied", async () => {
  const { reader, writer } = pair();
  const t = new Transport(reader, writer);
  const api = createExtensionAPI(t, { name: "ext", version: "0", requestedCapabilities: [] }, []);
  await assert.rejects(api.exec("ls", []), CapabilityDeniedError);
  t.close();
});

test("tool_execute dispatch routes to registered tool and returns result", async () => {
  const { reader, writer, written } = pair();
  const t = new Transport(reader, writer);
  const api = createExtensionAPI(t, { name: "ext", version: "0", requestedCapabilities: [] },
    [{ service: "tools", version: 1, methods: ["register"] }]);
  api.registerTool({
    name: "greet",
    label: "g",
    description: "d",
    parameters: {},
    execute: async (callId, args, _sig, onUpdate) => {
      onUpdate?.({ content: [{ type: "text", text: "progress" }] });
      const p = args as { who: string };
      return { content: [{ type: "text", text: `hello, ${p.who}` }] };
    },
  });
  // Wait for and settle the register host_call.
  await new Promise((r) => setImmediate(r));
  const registerReq = parseLines(written)[0] as { id: number };
  reader.push(JSON.stringify({ jsonrpc: "2.0", id: registerReq.id, result: null }) + "\n");
  // Fire a tool_execute event.
  reader.push(JSON.stringify({
    jsonrpc: "2.0", id: 200, method: "pi.extension/extension_event",
    params: { event: "tool_execute", payload: { tool_call_id: "tc-1", name: "greet", args: { who: "world" } } },
  }) + "\n");
  await new Promise((r) => setImmediate(r));
  await new Promise((r) => setImmediate(r));
  const all = parseLines(written);
  const update = all.find((m) => m.method === "pi.extension/tool_update") as
    { params: { tool_call_id: string; partial: { content: Array<{ text: string }> } } };
  assert.equal(update.params.tool_call_id, "tc-1");
  assert.equal(update.params.partial.content[0].text, "progress");
  const evtResp = all.find((m) => m.id === 200) as
    { result: { content: Array<{ text: string }> } };
  assert.equal(evtResp.result.content[0].text, "hello, world");
  t.close();
});

test("tool_execute for unknown tool returns isError", async () => {
  const { reader, writer, written } = pair();
  const t = new Transport(reader, writer);
  createExtensionAPI(t, { name: "ext", version: "0", requestedCapabilities: [] }, []);
  reader.push(JSON.stringify({
    jsonrpc: "2.0", id: 201, method: "pi.extension/extension_event",
    params: { event: "tool_execute", payload: { tool_call_id: "tc-2", name: "missing", args: {} } },
  }) + "\n");
  await new Promise((r) => setImmediate(r));
  await new Promise((r) => setImmediate(r));
  const evtResp = parseLines(written).find((m) => m.id === 201) as
    { result: { isError: boolean; content: Array<{ text: string }> } };
  assert.equal(evtResp.result.isError, true);
  assert.match(evtResp.result.content[0].text, /unknown tool: missing/);
  t.close();
});

test("tool_execute surfaces execute errors with isError", async () => {
  const { reader, writer, written } = pair();
  const t = new Transport(reader, writer);
  const api = createExtensionAPI(t, { name: "ext", version: "0", requestedCapabilities: [] },
    [{ service: "tools", version: 1, methods: ["register"] }]);
  api.registerTool({
    name: "bad",
    label: "b",
    description: "d",
    parameters: {},
    execute: async () => { throw new Error("tool explode"); },
  });
  await new Promise((r) => setImmediate(r));
  const registerReq = parseLines(written)[0] as { id: number };
  reader.push(JSON.stringify({ jsonrpc: "2.0", id: registerReq.id, result: null }) + "\n");
  reader.push(JSON.stringify({
    jsonrpc: "2.0", id: 202, method: "pi.extension/extension_event",
    params: { event: "tool_execute", payload: { tool_call_id: "tc-3", name: "bad", args: {} } },
  }) + "\n");
  await new Promise((r) => setImmediate(r));
  await new Promise((r) => setImmediate(r));
  const evtResp = parseLines(written).find((m) => m.id === 202) as
    { result: { isError: boolean; content: Array<{ text: string }> } };
  assert.equal(evtResp.result.isError, true);
  assert.match(evtResp.result.content[0].text, /tool explode/);
  t.close();
});

test("extension_event for unknown event returns null control", async () => {
  const { reader, writer, written } = pair();
  const t = new Transport(reader, writer);
  createExtensionAPI(t, { name: "ext", version: "0", requestedCapabilities: [] }, []);
  reader.push(JSON.stringify({
    jsonrpc: "2.0", id: 300, method: "pi.extension/extension_event",
    params: { event: "unknown_event", payload: {} },
  }) + "\n");
  await new Promise((r) => setImmediate(r));
  await new Promise((r) => setImmediate(r));
  const resp = parseLines(written).find((m) => m.id === 300) as
    { result: { control: null } };
  assert.deepEqual(resp.result, { control: null });
  t.close();
});

test("NotImplementedError and CapabilityDeniedError carry metadata", () => {
  const a = new NotImplementedError("foo", "#7");
  assert.equal(a.name, "NotImplementedError");
  assert.equal(a.method, "foo");
  assert.equal(a.spec, "#7");
  assert.match(a.message, /foo/);

  const b = new CapabilityDeniedError("svc.m", "nope");
  assert.equal(b.name, "CapabilityDeniedError");
  assert.equal(b.capability, "svc.m");
  assert.equal(b.reason, "nope");
  assert.match(b.message, /\(nope\)/);
});
