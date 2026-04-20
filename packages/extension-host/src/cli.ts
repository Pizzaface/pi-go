#!/usr/bin/env node
import {runExtensionHost} from "./runtime.js";

interface ParsedArgs {
  entry?: string;
  name?: string;
  cwd?: string;
}

function parseArgs(argv: string[]): ParsedArgs {
  const out: ParsedArgs = {};
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === "--entry") out.entry = argv[++i];
    else if (arg === "--name") out.name = argv[++i];
    else if (arg === "--cwd") out.cwd = argv[++i];
  }
  return out;
}

const args = parseArgs(process.argv.slice(2));
if (!args.entry) {
  process.stderr.write("go-pi-extension-host: --entry is required\n");
  process.exit(2);
}

const name = args.name ?? deriveName(args.entry);
const cwd = args.cwd ?? process.cwd();

function deriveName(entry: string): string {
  const base = entry.split(/[\\/]/).pop() ?? "extension";
  return base.replace(/\.[^.]+$/, "").replace(/[^a-z0-9_-]/gi, "-").toLowerCase() || "extension";
}

runExtensionHost({ entry: args.entry, name, cwd }).catch((err) => {
  process.stderr.write(`go-pi-extension-host: ${err instanceof Error ? err.message : String(err)}\n`);
  process.exit(1);
});
