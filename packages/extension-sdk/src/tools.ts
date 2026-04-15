import type { SourceInfo } from "./types.js";

export interface ContentPart {
  type: "text";
  text: string;
}

export interface ToolResult {
  content: ContentPart[];
  details?: Record<string, unknown>;
  isError?: boolean;
}

export type UpdateFn = (partial: ToolResult) => void;

export interface ToolDescriptor<TParams = unknown> {
  name: string;
  label: string;
  description: string;
  promptSnippet?: string;
  promptGuidelines?: string[];
  parameters: unknown; // JSON Schema
  prepareArguments?: (args: unknown) => unknown;
  execute: (
    toolCallId: string,
    params: TParams,
    signal: AbortSignal | undefined,
    onUpdate: UpdateFn | undefined,
    ctx: unknown,
  ) => Promise<ToolResult>;
}

export interface ToolInfo {
  name: string;
  description: string;
  parameters: unknown;
  sourceInfo: SourceInfo;
}
