import { execFileSync } from "node:child_process";

export const supportedTargets = Object.freeze({
  "aarch64-apple-darwin": { goos: "darwin", goarch: "arm64", extension: "" },
  "x86_64-apple-darwin": { goos: "darwin", goarch: "amd64", extension: "" },
  "x86_64-pc-windows-msvc": { goos: "windows", goarch: "amd64", extension: ".exe" },
  "aarch64-pc-windows-msvc": { goos: "windows", goarch: "arm64", extension: ".exe" },
  "x86_64-unknown-linux-gnu": { goos: "linux", goarch: "amd64", extension: "" },
  "aarch64-unknown-linux-gnu": { goos: "linux", goarch: "arm64", extension: "" },
});

export function hostTarget() {
  return execFileSync("rustc", ["--print", "host-tuple"], { encoding: "utf8" }).trim();
}

export function targetMetadata(target = hostTarget()) {
  const metadata = supportedTargets[target];
  if (!metadata) {
    const supported = Object.keys(supportedTargets).join(", ");
    throw new Error(`unsupported desktop target ${target}; supported targets: ${supported}`);
  }
  return { target, ...metadata, sidecarFilename: `agentmetry-${target}${metadata.extension}` };
}
