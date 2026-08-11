import assert from "node:assert/strict";
import test from "node:test";
import { targetMetadata } from "./targets.mjs";

test("maps desktop targets to Go and Tauri sidecar metadata", () => {
  assert.deepEqual(targetMetadata("aarch64-apple-darwin"), {
    target: "aarch64-apple-darwin",
    goos: "darwin",
    goarch: "arm64",
    extension: "",
    sidecarFilename: "agentmetry-aarch64-apple-darwin",
  });
  assert.deepEqual(targetMetadata("x86_64-pc-windows-msvc"), {
    target: "x86_64-pc-windows-msvc",
    goos: "windows",
    goarch: "amd64",
    extension: ".exe",
    sidecarFilename: "agentmetry-x86_64-pc-windows-msvc.exe",
  });
  assert.deepEqual(targetMetadata("aarch64-unknown-linux-gnu"), {
    target: "aarch64-unknown-linux-gnu",
    goos: "linux",
    goarch: "arm64",
    extension: "",
    sidecarFilename: "agentmetry-aarch64-unknown-linux-gnu",
  });
});

test("rejects targets without an explicit build mapping", () => {
  assert.throws(
    () => targetMetadata("unknown-target"),
    /unsupported desktop target unknown-target/,
  );
});
