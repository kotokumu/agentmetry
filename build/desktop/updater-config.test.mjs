import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { resolve } from "node:path";

import {
  releaseVersion,
  validateUpdaterConfig,
} from "./updater-config.mjs";

const root = resolve(import.meta.dirname, "../..");

test("release tags use the configured application version", () => {
  assert.equal(releaseVersion("v1.2.3", "1.2.3"), "1.2.3");
});

test("release tags reject a different application version", () => {
  assert.throws(
    () => releaseVersion("v1.2.4", "1.2.3"),
    /does not match Tauri version/,
  );
});

test("release tags must contain a semantic version", () => {
  assert.throws(() => releaseVersion("latest", "1.2.3"), /must match v/);
});

test("the checked-in Tauri configuration enables signed GitHub updates", () => {
  const config = JSON.parse(
    readFileSync(resolve(root, "src-tauri", "tauri.conf.json"), "utf8"),
  );

  assert.deepEqual(validateUpdaterConfig(config), {
    endpoint:
      "https://github.com/kotokumu/agentmetry/releases/latest/download/latest.json",
    createsArtifacts: true,
  });
});

test("the remote dashboard can invoke only the two app update commands", () => {
  const capability = JSON.parse(
    readFileSync(
      resolve(
        root,
        "src-tauri",
        "capabilities",
        "dashboard-updates.json",
      ),
      "utf8",
    ),
  );

  assert.deepEqual(capability.remote, {
    urls: ["http://127.0.0.1:17890/*"],
  });
  assert.deepEqual(capability.windows, ["main"]);
  assert.deepEqual(capability.permissions, [
    "core:event:allow-listen",
    "core:event:allow-unlisten",
    "allow-check-for-app-update",
    "allow-install-app-update",
  ]);
});
