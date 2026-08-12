import assert from "node:assert/strict";
import test from "node:test";

import { macosDmgAssetName } from "./release-assets.mjs";

test("names the Apple silicon DMG with a user-facing arm64 architecture", () => {
  assert.equal(
    macosDmgAssetName("1.0.0", "aarch64-apple-darwin"),
    "Agentmetry-v1.0.0-macos-arm64.dmg",
  );
});

test("names the Intel DMG with a user-facing x64 architecture", () => {
  assert.equal(
    macosDmgAssetName("1.0.0", "x86_64-apple-darwin"),
    "Agentmetry-v1.0.0-macos-x64.dmg",
  );
});

test("rejects unsupported macOS targets", () => {
  assert.throws(
    () => macosDmgAssetName("1.0.0", "armv7-apple-darwin"),
    /unsupported macOS target/,
  );
});
