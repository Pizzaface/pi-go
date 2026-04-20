import type {
  CommandDescriptor, ShortcutDescriptor, FlagDescriptor,
  ProviderDescriptor, RendererDescriptor, CommandInfo,
} from "./extended.js";
import type { ToolDescriptor, ToolInfo } from "./tools.js";
import type { EventHandler, EventName } from "./events.js";
import type {
  ModelRef, ExecOptions, ExecResult, ThinkingLevel,
} from "./types.js";

export interface CustomMessage {
  customType: string;
  content: string;
  display?: boolean;
  details?: Record<string, unknown>;
}

export interface SendOptions {
  deliverAs?: "steer" | "followUp" | "nextTurn";
  triggerTurn?: boolean;
}

export interface EventBus {
  on(event: string, handler: (data: unknown) => void): void;
  emit(event: string, data: unknown): void;
}

export interface ExtensionAPI {
  name(): string;
  version(): string;

  registerTool(desc: ToolDescriptor): void;
  unregisterTool(name: string): void;
  ready(): void;
  registerCommand(name: string, desc: CommandDescriptor): void;
  registerShortcut(shortcut: string, desc: ShortcutDescriptor): void;
  registerFlag(name: string, desc: FlagDescriptor): void;
  registerProvider(name: string, config: ProviderDescriptor): void;
  unregisterProvider(name: string): void;
  registerMessageRenderer(customType: string, renderer: RendererDescriptor): void;

  on<E extends EventName>(event: E, handler: EventHandler<unknown>): void;
  events: EventBus;

  sendMessage(msg: CustomMessage, opts?: SendOptions): void;
  sendUserMessage(content: string, opts?: SendOptions): void;
  appendEntry(customType: string, data?: unknown): void;
  setSessionName(name: string): void;
  getSessionName(): string | undefined;
  setLabel(entryId: string, label: string | undefined): void;

  getActiveTools(): string[];
  getAllTools(): ToolInfo[];
  setActiveTools(names: string[]): void;
  setModel(model: ModelRef): Promise<boolean>;
  getThinkingLevel(): ThinkingLevel;
  setThinkingLevel(level: ThinkingLevel): void;

  exec(cmd: string, args: string[], opts?: ExecOptions): Promise<ExecResult>;
  getCommands(): CommandInfo[];
  getFlag(name: string): unknown;
}
