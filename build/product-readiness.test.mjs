import assert from "node:assert/strict";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");

const currentDocuments = [
  "README.md",
  "CONTRIBUTING.md",
  "SECURITY.md",
  "docs/README.md",
  "docs/architecture.md",
  "docs/operations/storage-maintenance.md",
];

const currentProductSurfaces = [
  ...currentDocuments,
  "web/index.html",
  "web/src/app/agentmetry-app.ts",
  "internal/transport/mcpserver/server.go",
];

function read(path) {
  return readFileSync(resolve(repositoryRoot, path), "utf8");
}

function markdownDocuments(directory = "docs") {
  return readdirSync(resolve(repositoryRoot, directory), { withFileTypes: true }).flatMap((entry) => {
    const path = `${directory}/${entry.name}`;
    return entry.isDirectory() ? markdownDocuments(path) : entry.name.endsWith(".md") ? [path] : [];
  });
}

test("current product surfaces exist and do not present Agentmetry as an experiment", () => {
  for (const path of currentProductSurfaces) {
    assert.ok(existsSync(resolve(repositoryRoot, path)), `${path} must exist`);
    assert.doesNotMatch(read(path), /\b(?:PoC|prototype|lab)\b|proof[ -]of[ -]concept|early development/i, `${path} contains experimental product language`);
  }

  assert.ok(existsSync(resolve(repositoryRoot, "docs/archive/initial-vertical-slice.md")));
  assert.ok(existsSync(resolve(repositoryRoot, "docs/archive/initial-product-architecture.md")));
  assert.equal(existsSync(resolve(repositoryRoot, "docs/poc-spec.md")), false);
  assert.equal(existsSync(resolve(repositoryRoot, "docs/grand-architecture.md")), false);
});

test("local Markdown links resolve", () => {
  const markdownLink = /\[[^\]]*\]\(([^)]+)\)/g;

  for (const path of ["README.md", "CONTRIBUTING.md", "SECURITY.md", ...markdownDocuments()]) {
    const source = read(path);
    for (const match of source.matchAll(markdownLink)) {
      const target = match[1];
      if (/^(?:https?:|mailto:|#)/.test(target)) continue;
      const withoutAnchor = decodeURIComponent(target.split("#", 1)[0]);
      if (!withoutAnchor) continue;
      assert.ok(existsSync(resolve(repositoryRoot, dirname(path), withoutAnchor)), `${path} links to missing ${target}`);
    }
  }
});

test("release metadata stays synchronized", () => {
  const applicationVersion = JSON.parse(read("src-tauri/tauri.conf.json")).version;
  const releaseVersion = JSON.parse(read(".release-please-manifest.json"))["."];
  const cargoVersion = read("src-tauri/Cargo.toml").match(/^version = "([^"]+)"/m)?.[1];
  const cargoLockVersion = read("src-tauri/Cargo.lock").match(/^name = "agentmetry-desktop"\nversion = "([^"]+)"/m)?.[1];
  const productVersion = read("internal/product/metadata.go").match(/^\s*Version\s*=\s*"([^"]+)"/m)?.[1];

  assert.match(applicationVersion, /^\d+\.\d+\.\d+$/);
  assert.equal(releaseVersion, applicationVersion);
  assert.equal(cargoVersion, applicationVersion);
  assert.equal(cargoLockVersion, applicationVersion);
  assert.equal(productVersion, applicationVersion);
});
