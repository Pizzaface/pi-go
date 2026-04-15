import type { Transport } from "./transport.js";
import type { ExtensionAPI, CustomMessage, SendOptions, EventBus } from "./api.js";
import type { ToolDescriptor, ToolInfo, ToolResult, UpdateFn } from "./tools.js";
import type {
  CommandDescriptor, ShortcutDescriptor, FlagDescriptor,
  ProviderDescriptor, RendererDescriptor, CommandInfo,
} from "./extended.js";
import type { EventHandler, EventName, SessionStartEvent } from "./events.js";
import { EventNames } from "./events.js";
import type { Metadata, ModelRef, ExecOptions, ExecResult, ThinkingLevel } from "./types.js";
import { NotImplementedError, CapabilityDeniedError } from "./errors.js";

export interface GrantedService {
  service: string;
  version: number;
  methods: string[];
}

export function createExtensionAPI(
  transport: Transport,
  metadata: Metadata,
  granted: GrantedService[],
): ExtensionAPI {
  const grantMap = new Map<string, Set<string>>();
  for (const g of granted) {
    grantMap.set(g.service, new Set(g.methods));
  }
  const tools = new Map<string, ToolDescriptor>();
  const handlers = new Map<string, EventHandler<unknown>[]>();

  function ensureGrant(service: string, method: string): void {
    const set = grantMap.get(service);
    if (!set || !set.has(method)) {
      throw new CapabilityDeniedError(`${service}.${method}`);
    }
  }

  async function hostCall(capability: string, payload: unknown): Promise<unknown> {
    const [service, method] = capability.split(".");
    ensureGrant(service, method);
    return transport.call("pi.extension/host_call", {
      service, version: 1, method, payload,
    });
  }

  transport.handle("pi.extension/extension_event", async (params) => {
    const p = params as { event: string; payload: unknown };
    if (p.event === EventNames.ToolExecute) {
      return handleToolExecute(tools, p.payload, transport);
    }
    if (p.event === EventNames.SessionStart) {
      const evt = p.payload as SessionStartEvent;
      for (const h of handlers.get(p.event) ?? []) {
        const r = await h(evt, null);
        if (r && typeof r === "object" && "control" in r && r.control) {
          return { control: r.control };
        }
      }
      return { control: null };
    }
    return { control: null };
  });

  const notImpl = (method: string, spec: string) => () => { throw new NotImplementedError(method, spec); };

  return {
    name: () => metadata.name,
    version: () => metadata.version,

    registerTool: (desc) => {
      if (!desc.name || !desc.execute) throw new Error("registerTool: name and execute are required");
      ensureGrant("tools", "register");
      tools.set(desc.name, desc);
      transport.call("pi.extension/host_call", {
        service: "tools",
        version: 1,
        method: "register",
        payload: {
          name: desc.name,
          label: desc.label,
          description: desc.description,
          prompt_snippet: desc.promptSnippet,
          prompt_guidelines: desc.promptGuidelines,
          parameters: desc.parameters,
        },
      }).catch((err) => { throw err; });
    },
    registerCommand: notImpl("registerCommand", "#2") as (name: string, desc: CommandDescriptor) => void,
    registerShortcut: notImpl("registerShortcut", "#6") as (s: string, d: ShortcutDescriptor) => void,
    registerFlag: notImpl("registerFlag", "#6") as (n: string, d: FlagDescriptor) => void,
    registerProvider: notImpl("registerProvider", "#6") as (n: string, c: ProviderDescriptor) => void,
    unregisterProvider: notImpl("unregisterProvider", "#6") as (n: string) => void,
    registerMessageRenderer: notImpl("registerMessageRenderer", "#6") as (t: string, r: RendererDescriptor) => void,

    on: (event, handler) => {
      if (event !== EventNames.SessionStart) {
        throw new NotImplementedError(`on(${event})`, "#3");
      }
      ensureGrant("events", "session_start");
      const list = handlers.get(event) ?? [];
      list.push(handler as EventHandler<unknown>);
      handlers.set(event, list);
      transport.call("pi.extension/subscribe_event", {
        events: [{ name: event, version: 1 }],
      });
    },

    events: {
      on: notImpl("events.on", "#3") as (event: string, handler: (d: unknown) => void) => void,
      emit: notImpl("events.emit", "#3") as (event: string, data: unknown) => void,
    } as EventBus,

    sendMessage: notImpl("sendMessage", "#5") as (m: CustomMessage, o?: SendOptions) => void,
    sendUserMessage: notImpl("sendUserMessage", "#5") as (c: string, o?: SendOptions) => void,
    appendEntry: notImpl("appendEntry", "#5") as (t: string, d?: unknown) => void,
    setSessionName: notImpl("setSessionName", "#5") as (n: string) => void,
    getSessionName: () => undefined,
    setLabel: notImpl("setLabel", "#5") as (e: string, l: string | undefined) => void,

    getActiveTools: () => [],
    getAllTools: (): ToolInfo[] => [],
    setActiveTools: notImpl("setActiveTools", "#3") as (names: string[]) => void,
    setModel: async () => { throw new NotImplementedError("setModel", "#3"); },
    getThinkingLevel: () => "off" as ThinkingLevel,
    setThinkingLevel: notImpl("setThinkingLevel", "#3") as (l: ThinkingLevel) => void,

    exec: async (cmd: string, args: string[], opts?: ExecOptions): Promise<ExecResult> => {
      ensureGrant("exec", "shell");
      const result = (await hostCall("exec.shell", { cmd, args, timeout: opts?.timeout })) as ExecResult;
      return result;
    },
    getCommands: (): CommandInfo[] => [],
    getFlag: () => undefined,
  };
}

async function handleToolExecute(
  tools: Map<string, ToolDescriptor>,
  payload: unknown,
  transport: Transport,
): Promise<ToolResult> {
  const p = payload as { tool_call_id: string; name: string; args: unknown };
  const desc = tools.get(p.name);
  if (!desc) {
    return { content: [{ type: "text", text: `unknown tool: ${p.name}` }], isError: true };
  }
  const onUpdate: UpdateFn = (partial) => {
    transport.notify("pi.extension/tool_update", { tool_call_id: p.tool_call_id, partial });
  };
  try {
    return await desc.execute(p.tool_call_id, p.args, undefined, onUpdate, null);
  } catch (err) {
    return {
      content: [{ type: "text", text: err instanceof Error ? err.message : String(err) }],
      isError: true,
    };
  }
}
