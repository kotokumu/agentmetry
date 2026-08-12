import { execFileSync, spawnSync } from "node:child_process";
import { cpSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { tmpdir } from "node:os";
import { macosDmgAssetName } from "./release-assets.mjs";
import { hostTarget } from "./targets.mjs";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const config = JSON.parse(readFileSync(resolve(root, "src-tauri", "tauri.conf.json"), "utf8"));
const target = process.argv.includes("--target")
  ? process.argv[process.argv.indexOf("--target") + 1]
  : hostTarget();
const app = resolve(root, "src-tauri", "target", "release", "bundle", "macos", "Agentmetry.app");
const outputDir = resolve(root, "src-tauri", "target", "release", "bundle", "dmg");
const output = join(outputDir, macosDmgAssetName(config.version, target));

if (!existsSync(app)) {
  throw new Error(`macOS application bundle is missing: ${app}`);
}

mkdirSync(outputDir, { recursive: true });
const staging = mkdtempSync(join(tmpdir(), "agentmetry-dmg-"));
try {
  cpSync(app, join(staging, "Agentmetry.app"), { recursive: true });
  const result = spawnSync(
    "hdiutil",
    ["create", "-volname", "Agentmetry", "-srcfolder", staging, "-ov", "-format", "UDZO", output],
    { cwd: root, stdio: "inherit" },
  );
  if (result.status !== 0) {
    process.exit(result.status ?? 1);
  }
  console.log(`built ${output}`);
} finally {
  rmSync(staging, { recursive: true, force: true });
}
