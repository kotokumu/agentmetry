import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { validateMacosEntitlements } from "./macos-signing.mjs";
import { releaseVersion, validateUpdaterConfig } from "./updater-config.mjs";

const root = resolve(import.meta.dirname, "../..");
const config = JSON.parse(
  readFileSync(resolve(root, "src-tauri", "tauri.conf.json"), "utf8"),
);
const tag = process.argv[2];

if (!tag) {
  throw new Error("usage: node build/desktop/verify-release.mjs <release-tag>");
}

releaseVersion(tag, config.version);
validateUpdaterConfig(config);

const entitlements = config.bundle?.macOS?.entitlements;
validateMacosEntitlements(
  entitlements === undefined
    ? undefined
    : readFileSync(resolve(root, "src-tauri", entitlements), "utf8"),
);
