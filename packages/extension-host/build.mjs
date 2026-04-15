import { build } from "esbuild";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";

const __dirname = dirname(fileURLToPath(import.meta.url));

await build({
  entryPoints: [resolve(__dirname, "src/cli.ts")],
  bundle: true,
  platform: "node",
  target: "node20",
  format: "esm",
  outfile: resolve(__dirname, "dist/host.bundle.js"),
  external: ["jiti"],
  sourcemap: false,
  minify: true,
});

console.log("Built dist/host.bundle.js");
