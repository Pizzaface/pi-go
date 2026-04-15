export type { ExtensionAPI, CustomMessage, SendOptions, EventBus } from "./api.js";
export type {
  ToolDescriptor, ToolResult, ToolInfo, ContentPart, UpdateFn,
} from "./tools.js";
export type {
  EventHandler, EventName, EventResult, EventControl,
  SessionStartEvent,
} from "./events.js";
export { EventNames } from "./events.js";
export type {
  Metadata, ModelRef, ContextUsage, ThinkingLevel,
  ExecOptions, ExecResult, SourceInfo,
} from "./types.js";
export type {
  CommandDescriptor, ShortcutDescriptor, FlagDescriptor,
  ProviderDescriptor, RendererDescriptor, CommandInfo, AutocompleteItem,
} from "./extended.js";
export { Transport, connectStdio } from "./transport.js";
export { createExtensionAPI } from "./api-impl.js";
export type { GrantedService } from "./api-impl.js";
export { NotImplementedError, CapabilityDeniedError } from "./errors.js";
// Re-export TypeBox for parameter schemas.
export { Type } from "@sinclair/typebox";
