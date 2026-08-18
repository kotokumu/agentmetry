import { spawnSync } from "node:child_process";
import { chmodSync, mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { targetMetadata } from "./targets.mjs";

const currentFile = fileURLToPath(import.meta.url);
const root = resolve(dirname(currentFile), "../..");

export function sidecarBuildEnvironment(target, inheritedEnvironment = process.env) {
  const metadata = targetMetadata(target);
  return {
    ...inheritedEnvironment,
    CGO_ENABLED: "0",
    GOOS: metadata.goos,
    GOARCH: metadata.goarch,
  };
}

export function buildSidecar(target) {
  const metadata = targetMetadata(target);
  const output = resolve(root, "src-tauri", "binaries", metadata.sidecarFilename);
  mkdirSync(dirname(output), { recursive: true });

  const result = spawnSync(
    "go",
    ["build", "-trimpath", "-o", output, "./cmd/agentmetry"],
    {
      cwd: root,
      env: sidecarBuildEnvironment(metadata.target),
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

if (process.argv[1] && resolve(process.argv[1]) === currentFile) {
  const targetArgument = process.argv.indexOf("--target");
  const target = targetArgument >= 0 ? process.argv[targetArgument + 1] : undefined;
  buildSidecar(target);
}
