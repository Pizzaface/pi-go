import { createJiti } from "jiti";
import { pathToFileURL } from "node:url";

export interface LoadedExtension {
  register: (pi: unknown) => void | Promise<void>;
}

export async function loadExtension(entry: string, cwd: string): Promise<LoadedExtension> {
  const jiti = createJiti(pathToFileURL(cwd + "/").href, { interopDefault: true });
  const mod = (await jiti.import(entry)) as { default?: (pi: unknown) => void };
  if (typeof mod.default !== "function") {
    throw new Error(`extension at ${entry} does not export a default function`);
  }
  return { register: mod.default };
}
