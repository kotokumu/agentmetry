import assert from "node:assert/strict";
import test from "node:test";

import { validateMacosEntitlements } from "./macos-signing.mjs";

test("accepts Developer ID builds without an entitlements file", () => {
  assert.doesNotThrow(() => validateMacosEntitlements(undefined));
});

test("rejects App Sandbox when the bundle contains a command-line sidecar", () => {
  assert.throws(
    () =>
      validateMacosEntitlements(`
        <plist version="1.0">
          <dict>
            <key>com.apple.security.app-sandbox</key>
            <true/>
          </dict>
        </plist>
      `),
    /must not enable com\.apple\.security\.app-sandbox/,
  );
});

test("accepts unrelated macOS entitlements", () => {
  assert.doesNotThrow(() =>
    validateMacosEntitlements(`
      <plist version="1.0">
        <dict>
          <key>com.apple.security.cs.allow-jit</key>
          <true/>
        </dict>
      </plist>
    `),
  );
});
