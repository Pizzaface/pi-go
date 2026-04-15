import { Readable, Writable } from "node:stream";
import { createInterface } from "node:readline";

export type RequestHandler = (params: unknown) => unknown | Promise<unknown>;

export class Transport {
  private nextId = 1;
  private pending = new Map<number, { resolve: (v: unknown) => void; reject: (e: Error) => void }>();
  private handlers = new Map<string, RequestHandler>();
  private closed = false;

  constructor(
    private readonly input: Readable,
    private readonly output: Writable,
  ) {
    const rl = createInterface({ input });
    rl.on("line", (line) => {
      if (!line.trim()) return;
      try {
        const msg = JSON.parse(line);
        this.handleMessage(msg);
      } catch {
        // ignore malformed
      }
    });
  }

  call<T = unknown>(method: string, params: unknown): Promise<T> {
    if (this.closed) return Promise.reject(new Error("transport closed"));
    const id = this.nextId++;
    return new Promise<T>((resolve, reject) => {
      this.pending.set(id, { resolve: resolve as (v: unknown) => void, reject });
      this.write({ jsonrpc: "2.0", id, method, params });
    });
  }

  notify(method: string, params: unknown): void {
    if (this.closed) return;
    this.write({ jsonrpc: "2.0", method, params });
  }

  handle(method: string, handler: RequestHandler): void {
    this.handlers.set(method, handler);
  }

  close(): void {
    this.closed = true;
    for (const { reject } of this.pending.values()) {
      reject(new Error("transport closed"));
    }
    this.pending.clear();
  }

  private write(obj: unknown): void {
    this.output.write(JSON.stringify(obj) + "\n");
  }

  private async handleMessage(msg: {
    id?: number;
    method?: string;
    params?: unknown;
    result?: unknown;
    error?: { code: number; message: string };
  }): Promise<void> {
    if (msg.method) {
      const handler = this.handlers.get(msg.method);
      if (msg.id === undefined) {
        if (handler) await handler(msg.params);
        return;
      }
      try {
        const result = handler ? await handler(msg.params) : null;
        this.write({ jsonrpc: "2.0", id: msg.id, result });
      } catch (err) {
        this.write({
          jsonrpc: "2.0",
          id: msg.id,
          error: { code: -32603, message: err instanceof Error ? err.message : String(err) },
        });
      }
      return;
    }
    if (msg.id !== undefined) {
      const entry = this.pending.get(msg.id);
      if (!entry) return;
      this.pending.delete(msg.id);
      if (msg.error) {
        entry.reject(new Error(`rpc ${msg.error.code}: ${msg.error.message}`));
      } else {
        entry.resolve(msg.result);
      }
    }
  }
}

export function connectStdio(): Transport {
  return new Transport(process.stdin, process.stdout);
}
