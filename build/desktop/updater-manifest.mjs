import { readFileSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

const currentFile = fileURLToPath(import.meta.url);

const platformAssets = [
  ["darwin-aarch64", "macos-arm64.app.tar.gz"],
  ["darwin-aarch64-app", "macos-arm64.app.tar.gz"],
  ["linux-x86_64", "linux-x64.AppImage"],
  ["linux-x86_64-appimage", "linux-x64.AppImage"],
  ["linux-x86_64-deb", "linux-x64.deb"],
  ["windows-x86_64", "windows-x64-setup.exe"],
  ["windows-x86_64-nsis", "windows-x64-setup.exe"],
];

function requireNonEmptyString(value, label) {
  if (typeof value !== "string" || value.trim() === "") {
    throw new Error(`${label} must be a non-empty string`);
  }
  return value.trim();
}

export function normalizeUpdaterManifest(manifest, { repository, tag, signatures }) {
  if (!manifest || typeof manifest !== "object" || Array.isArray(manifest)) {
    throw new Error("updater manifest must be an object");
  }

  const version = requireNonEmptyString(manifest.version, "manifest version");
  const releaseTag = requireNonEmptyString(tag, "release tag");
  if (releaseTag !== `v${version}`) {
    throw new Error(`release tag ${releaseTag} does not match manifest version ${version}`);
  }

  const releaseRepository = requireNonEmptyString(repository, "repository");
  if (!/^[^/\s]+\/[^/\s]+$/.test(releaseRepository)) {
    throw new Error("repository must use the owner/repository format");
  }

  const existingPlatforms =
    manifest.platforms && typeof manifest.platforms === "object" && !Array.isArray(manifest.platforms)
      ? manifest.platforms
      : {};
  const requiredPlatformNames = new Set(platformAssets.map(([platform]) => platform));
  const platforms = Object.fromEntries(
    Object.entries(existingPlatforms).filter(([platform]) => !requiredPlatformNames.has(platform)),
  );
  const releaseBase = `https://github.com/${releaseRepository}/releases/download/${releaseTag}`;

  for (const [platform, assetSuffix] of platformAssets) {
    const assetName = `Agentmetry-v${version}-${assetSuffix}`;
    const signatureName = `${assetName}.sig`;
    const signature = requireNonEmptyString(
      signatures?.[signatureName],
      `missing updater signature: ${signatureName}`,
    );
    platforms[platform] = {
      signature,
      url: `${releaseBase}/${assetName}`,
    };
  }

  return { ...manifest, platforms };
}

export function normalizeUpdaterManifestFile({
  manifestPath,
  assetsDirectory,
  repository,
  tag,
}) {
  const manifest = JSON.parse(readFileSync(manifestPath, "utf8"));
  const version = requireNonEmptyString(manifest.version, "manifest version");
  const signatureNames = new Set(
    platformAssets.map(([, assetSuffix]) => `Agentmetry-v${version}-${assetSuffix}.sig`),
  );
  const signatures = Object.fromEntries(
    [...signatureNames].map((name) => [name, readFileSync(resolve(assetsDirectory, name), "utf8")]),
  );
  const normalized = normalizeUpdaterManifest(manifest, { repository, tag, signatures });
  writeFileSync(manifestPath, `${JSON.stringify(normalized, null, 2)}\n`);
  return normalized;
}

if (process.argv[1] && resolve(process.argv[1]) === currentFile) {
  const [manifestPath, assetsDirectory, repository, tag] = process.argv.slice(2);
  if (!manifestPath || !assetsDirectory || !repository || !tag) {
    console.error(
      "usage: node build/desktop/updater-manifest.mjs <manifest> <assets-dir> <owner/repository> <tag>",
    );
    process.exitCode = 1;
  } else {
    try {
      normalizeUpdaterManifestFile({ manifestPath, assetsDirectory, repository, tag });
    } catch (error) {
      console.error(error instanceof Error ? error.message : error);
      process.exitCode = 1;
    }
  }
}
