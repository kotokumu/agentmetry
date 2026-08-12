const MACOS_ARCHITECTURES = new Map([
  ["aarch64-apple-darwin", "arm64"],
  ["x86_64-apple-darwin", "x64"],
]);

export function macosDmgAssetName(version, target) {
  const architecture = MACOS_ARCHITECTURES.get(target);
  if (!architecture) {
    throw new Error(`unsupported macOS target: ${target}`);
  }
  return `Agentmetry-v${version}-macos-${architecture}.dmg`;
}
