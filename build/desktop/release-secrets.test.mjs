import assert from "node:assert/strict";
import test from "node:test";

import {
  missingReleaseSecrets,
  REQUIRED_RELEASE_SECRETS,
} from "./release-secrets.mjs";

const completeEnvironment = Object.fromEntries(
  REQUIRED_RELEASE_SECRETS.map((name) => [name, "configured"]),
);

test("accepts a release environment with updater and Apple credentials", () => {
  assert.deepEqual(missingReleaseSecrets(completeEnvironment), []);
});

test("reports every missing or blank release credential", () => {
  const environment = {
    ...completeEnvironment,
    APPLE_CERTIFICATE: "",
    APPLE_ID: "   ",
  };
  delete environment.TAURI_SIGNING_PRIVATE_KEY;

  assert.deepEqual(missingReleaseSecrets(environment), [
    "TAURI_SIGNING_PRIVATE_KEY",
    "APPLE_CERTIFICATE",
    "APPLE_ID",
  ]);
});

test("defines every credential required by signed and notarized macOS CD", () => {
  assert.deepEqual(REQUIRED_RELEASE_SECRETS, [
    "TAURI_SIGNING_PRIVATE_KEY",
    "APPLE_CERTIFICATE",
    "APPLE_CERTIFICATE_PASSWORD",
    "KEYCHAIN_PASSWORD",
    "APPLE_ID",
    "APPLE_PASSWORD",
    "APPLE_TEAM_ID",
  ]);
});
