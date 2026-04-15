export interface Metadata {
  name: string;
  version: string;
  description?: string;
  prompt?: string;
  requestedCapabilities: string[];
  entry?: string;
}

export interface ModelRef {
  provider: string;
  id: string;
}

export interface ContextUsage {
  tokens: number;
}

export type ThinkingLevel = "off" | "minimal" | "low" | "medium" | "high" | "xhigh";

export interface ExecOptions {
  signal?: AbortSignal;
  timeout?: number;
}

export interface ExecResult {
  stdout: string;
  stderr: string;
  code: number;
  killed: boolean;
}

export interface SourceInfo {
  path: string;
  source: string;
  scope: "user" | "project" | "temporary";
  origin: "package" | "top-level";
  baseDir?: string;
}
