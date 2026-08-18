import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { sidecarBuildEnvironment } from "./build-sidecar.mjs";

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), "../..");

function read(path) {
  return readFileSync(resolve(repositoryRoot, path), "utf8");
}

function occurrences(source, pattern) {
  return [...source.matchAll(pattern)].length;
}

test("uses the Go toolchain's cache location for sidecar builds", () => {
  const environment = sidecarBuildEnvironment("x86_64-unknown-linux-gnu", {
    PATH: "/usr/bin",
  });

  assert.deepEqual(environment, {
    PATH: "/usr/bin",
    CGO_ENABLED: "0",
    GOOS: "linux",
    GOARCH: "amd64",
  });
  assert.equal(Object.hasOwn(environment, "GOCACHE"), false);
});

test("preserves an explicitly configured Go cache location", () => {
  const environment = sidecarBuildEnvironment("aarch64-apple-darwin", {
    GOCACHE: "/cache/go-build",
  });

  assert.equal(environment.GOCACHE, "/cache/go-build");
  assert.equal(environment.GOOS, "darwin");
  assert.equal(environment.GOARCH, "arm64");
});

test("CI builds release-profile dependencies into the default-branch cache", () => {
  const workflow = read(".github/workflows/ci.yml");
  const filters = read(".github/filters.yml");
  const toolchain = read("rust-toolchain.toml");

  assert.equal(occurrences(workflow, /uses: Swatinem\/rust-cache@v2/g), 2);
  assert.equal(occurrences(workflow, /shared-key: desktop-build/g), 2);
  assert.equal(occurrences(workflow, /workspaces: ["']?src-tauri -> target["']?/g), 2);
  assert.equal(occurrences(workflow, /uses: dtolnay\/rust-toolchain@1\.97\.1/g), 2);
  assert.doesNotMatch(workflow, /dtolnay\/rust-toolchain@stable/);
  assert.match(toolchain, /channel = "1\.97\.1"/);
  assert.match(workflow, /uses: dorny\/paths-filter@v4/);
  assert.match(workflow, /filters: \.github\/filters\.yml/);
  assert.match(workflow, /desktop:\n    name: Desktop build inputs\n    needs: changes\n    if: needs\.changes\.outputs\.desktop == 'true'/);
  assert.match(
    workflow,
    /desktop-release-cache:\n    name: Seed desktop cache \(\$\{\{ matrix\.name \}\}\)\n    needs: changes\n    if: github\.event_name == 'push' && needs\.changes\.outputs\.desktop == 'true'/,
  );
  assert.match(filters, /desktop:\n(?:.|\n)*- 'src-tauri\/\*\*'/);
  assert.match(filters, /- 'web\/\*\*'/);
  assert.match(filters, /- 'internal\/\*\*'/);
  assert.doesNotMatch(filters, /docs\/|openspec\//);
  assert.match(workflow, /desktop:\n(?:.|\n)*?    runs-on: ubuntu-22\.04/);
  assert.equal(
    occurrences(workflow, /cargo build --release --manifest-path src-tauri\/Cargo\.toml/g),
    2,
  );
  assert.equal(
    occurrences(workflow, /uses: actions\/setup-go@v5/g),
    occurrences(workflow, /cache-dependency-path: go\.sum/g),
  );
});

test("CD restores the release-profile dependency cache seeded on main", () => {
  const workflow = read(".github/workflows/release.yml");

  assert.match(workflow, /uses: Swatinem\/rust-cache@v2/);
  assert.match(workflow, /shared-key: desktop-build/);
  assert.match(workflow, /workspaces: ["']?src-tauri -> target["']?/);
  assert.match(workflow, /uses: dtolnay\/rust-toolchain@1\.97\.1/);
  assert.equal(
    occurrences(workflow, /uses: actions\/setup-go@v5/g),
    occurrences(workflow, /cache-dependency-path: go\.sum/g),
  );
});
