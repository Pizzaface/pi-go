import type { SourceInfo } from "./types.js";

export interface CommandDescriptor {
  description: string;
  handler: (args: string, ctx: unknown) => Promise<void> | void;
  getArgumentCompletions?: (prefix: string) => AutocompleteItem[] | null;
}

export interface AutocompleteItem {
  value: string;
  label: string;
}

export interface ShortcutDescriptor {
  description: string;
  handler: (ctx: unknown) => Promise<void> | void;
}

export interface FlagDescriptor {
  description: string;
  type: "boolean" | "string" | "number";
  default?: unknown;
}

export interface ProviderDescriptor {
  baseUrl?: string;
  apiKey?: string;
  api?: string;
  headers?: Record<string, string>;
  authHeader?: boolean;
}

export interface RendererDescriptor {
  kind: "text" | "markdown";
  handler: (message: unknown, options: unknown, theme: unknown) => unknown;
}

export interface CommandInfo {
  name: string;
  description?: string;
  source: "extension" | "prompt" | "skill";
  sourceInfo: SourceInfo;
}
