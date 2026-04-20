import type {ExtensionAPI, SessionStartEvent} from "@go-pi/extension-sdk";
import {EventNames, Type} from "@go-pi/extension-sdk";

export default async function register(pi: ExtensionAPI): Promise<void> {
  pi.on(EventNames.SessionStart, (evt: unknown) => {
    const e = evt as SessionStartEvent;
    console.log(`hosted-hello-ts: session_start reason=${e.reason}`);
    return { control: null };
  });

  pi.registerTool({
    name: "greet",
    label: "Greet",
    description: "Returns a friendly greeting.",
    parameters: Type.Object({
      name: Type.String({ description: "Name to greet" }),
    }),
    execute: async (_toolCallId, params) => {
      const args = (params ?? {}) as { name?: string };
      const name = args.name?.trim() || "world";
      return {
        content: [{ type: "text", text: `Hello, ${name}!` }],
      };
    },
  });
}
