import { spawnSync } from "node:child_process";
import { chmodSync, mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { tmpdir } from "node:os";
import { targetMetadata } from "./targets.mjs";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "../..");

export function buildSidecar(target) {
  const metadata = targetMetadata(target);
  const output = resolve(root, "src-tauri", "binaries", metadata.sidecarFilename);
  mkdirSync(dirname(output), { recursive: true });

  const result = spawnSync(
    "go",
    ["build", "-trimpath", "-o", output, "./cmd/agentmetry"],
    {
      cwd: root,
      env: {
        ...process.env,
        CGO_ENABLED: "0",
        GOCACHE: process.env.GOCACHE ?? resolve(tmpdir(), "agentmetry-go-cache"),
        GOOS: metadata.goos,
        GOARCH: metadata.goarch,
      },
      stdio: "inherit",
    },
  );

  if (result.status !== 0) {
    process.exit(result.status ?? 1);
  }
  if (!metadata.extension) {
    chmodSync(output, 0o755);
  }
  console.log(`built ${output}`);
  return output;
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const targetArgument = process.argv.indexOf("--target");
  const target = targetArgument >= 0 ? process.argv[targetArgument + 1] : undefined;
  buildSidecar(target);
}
