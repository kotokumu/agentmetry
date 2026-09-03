import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import {
  normalizeUpdaterManifest,
  normalizeUpdaterManifestFile,
} from "./updater-manifest.mjs";

const version = "1.11.0";
const tag = `v${version}`;
const repository = "kotokumu/agentmetry";

const signatures = {
  [`Agentmetry-v${version}-linux-x64.AppImage.sig`]: "linux-appimage-signature",
  [`Agentmetry-v${version}-linux-x64.deb.sig`]: "linux-deb-signature",
  [`Agentmetry-v${version}-macos-arm64.app.tar.gz.sig`]: "macos-signature",
  [`Agentmetry-v${version}-windows-x64-setup.exe.sig`]: "windows-signature",
};

test("restores every Tauri updater platform after parallel release jobs", () => {
  const manifest = {
    version,
    notes: "Release notes",
    pub_date: "2026-09-03T18:26:20.612Z",
    platforms: {
      "linux-x86_64": { signature: "stale", url: "stale" },
      "custom-platform": { signature: "custom", url: "custom" },
    },
  };

  const normalized = normalizeUpdaterManifest(manifest, {
    repository,
    tag,
    signatures,
  });

  const releaseBase = `https://github.com/${repository}/releases/download/${tag}`;
  assert.deepEqual(normalized, {
    ...manifest,
    platforms: {
      "custom-platform": { signature: "custom", url: "custom" },
      "darwin-aarch64": {
        signature: "macos-signature",
        url: `${releaseBase}/Agentmetry-v${version}-macos-arm64.app.tar.gz`,
      },
      "darwin-aarch64-app": {
        signature: "macos-signature",
        url: `${releaseBase}/Agentmetry-v${version}-macos-arm64.app.tar.gz`,
      },
      "linux-x86_64": {
        signature: "linux-appimage-signature",
        url: `${releaseBase}/Agentmetry-v${version}-linux-x64.AppImage`,
      },
      "linux-x86_64-appimage": {
        signature: "linux-appimage-signature",
        url: `${releaseBase}/Agentmetry-v${version}-linux-x64.AppImage`,
      },
      "linux-x86_64-deb": {
        signature: "linux-deb-signature",
        url: `${releaseBase}/Agentmetry-v${version}-linux-x64.deb`,
      },
      "windows-x86_64": {
        signature: "windows-signature",
        url: `${releaseBase}/Agentmetry-v${version}-windows-x64-setup.exe`,
      },
      "windows-x86_64-nsis": {
        signature: "windows-signature",
        url: `${releaseBase}/Agentmetry-v${version}-windows-x64-setup.exe`,
      },
    },
  });
});

test("requires the release version, tag, repository, and every updater signature", () => {
  const manifest = { version, platforms: {} };

  assert.throws(
    () => normalizeUpdaterManifest(manifest, { repository, tag: "v1.11.1", signatures }),
    /does not match manifest version/,
  );
  assert.throws(
    () => normalizeUpdaterManifest(manifest, { repository: "agentmetry", tag, signatures }),
    /owner\/repository/,
  );
  assert.throws(
    () => normalizeUpdaterManifest(manifest, { repository, tag, signatures: {} }),
    /missing updater signature.*macos-arm64\.app\.tar\.gz\.sig/,
  );
});

test("normalizes a downloaded manifest and signature directory in place", () => {
  const directory = mkdtempSync(join(tmpdir(), "agentmetry-updater-manifest-"));
  const manifestPath = join(directory, "latest.json");
  writeFileSync(
    manifestPath,
    JSON.stringify({ version, notes: "notes", pub_date: "date", platforms: {} }),
  );
  for (const [name, signature] of Object.entries(signatures)) {
    writeFileSync(join(directory, name), signature);
  }

  normalizeUpdaterManifestFile({ manifestPath, assetsDirectory: directory, repository, tag });

  const normalized = JSON.parse(readFileSync(manifestPath, "utf8"));
  assert.equal(normalized.platforms["darwin-aarch64-app"].signature, "macos-signature");
  assert.equal(normalized.platforms["windows-x86_64-nsis"].signature, "windows-signature");
  assert.equal(normalized.platforms["linux-x86_64-appimage"].signature, "linux-appimage-signature");
  assert.match(readFileSync(manifestPath, "utf8"), /\n$/);
});

test("normalizes the updater manifest after every platform build and before publishing", () => {
  const workflow = readFileSync(new URL("../../.github/workflows/release.yml", import.meta.url), "utf8");
  const normalizeStep = workflow.indexOf("node build/desktop/updater-manifest.mjs");
  const publishStep = workflow.indexOf('gh release edit "$GITHUB_REF_NAME"');

  assert.notEqual(normalizeStep, -1);
  assert.notEqual(publishStep, -1);
  assert.ok(normalizeStep < publishStep);
});
