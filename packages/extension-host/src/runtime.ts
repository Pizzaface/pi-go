import {
  connectStdio,
  createExtensionAPI,
  type GrantedService,
  type Metadata,
  type Transport,
} from "@go-pi/extension-sdk";
import {loadExtension} from "./loader.js";

export interface RuntimeOptions {
  entry: string;
  name: string;
  cwd: string;
}

export async function runExtensionHost(opts: RuntimeOptions): Promise<void> {
  const transport = connectStdio();
  redirectConsole(transport);

  const loaded = await loadExtension(opts.entry, opts.cwd);

  const metadata: Metadata = {
    name: opts.name,
    version: "0.0.0",
    requestedCapabilities: [],
  };

  const hsResult = (await transport.call("pi.extension/handshake", {
    protocol_version: "2.1",
    extension_id: metadata.name,
    extension_version: metadata.version,
    requested_services: [
      { service: "tools", version: 1, methods: ["register"] },
      { service: "events", version: 1, methods: ["session_start"] },
      { service: "exec", version: 1, methods: ["shell"] },
    ],
  })) as { granted_services: GrantedService[]; protocol_version: string };

  if (hsResult.protocol_version !== "2.1") {
    throw new Error(`unsupported protocol version: ${hsResult.protocol_version}`);
  }

  const api = createExtensionAPI(transport, metadata, hsResult.granted_services);
  await loaded.register(api);

  await new Promise<void>((resolve) => {
    transport.handle("pi.extension/shutdown", () => {
      resolve();
      return {};
    });
  });
}

function redirectConsole(transport: Transport): void {
  const redirect = (level: string) => (...args: unknown[]) => {
    const message = args.map((a) => (typeof a === "string" ? a : JSON.stringify(a))).join(" ");
    transport.notify("pi.extension/log", { level, message });
  };
  console.log = redirect("info");
  console.info = redirect("info");
  console.warn = redirect("warn");
  console.error = redirect("error");
}
