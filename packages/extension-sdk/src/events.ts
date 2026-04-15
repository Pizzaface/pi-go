export const EventNames = {
  SessionStart: "session_start",
  ToolExecute: "tool_execute",
} as const;

export type EventName = (typeof EventNames)[keyof typeof EventNames];

export interface SessionStartEvent {
  reason: "startup" | "reload" | "new" | "resume" | "fork";
  previousSessionFile?: string;
}

export interface EventControl {
  cancel?: boolean;
  block?: boolean;
  reason?: string;
  transform?: unknown;
  action?: string;
}

export interface EventResult {
  control?: EventControl | null;
}

export type EventHandler<TEvent> = (
  event: TEvent,
  ctx: unknown,
) => Promise<EventResult | void> | EventResult | void;
