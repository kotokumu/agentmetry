const appSandboxEntitlement =
  /<key>\s*com\.apple\.security\.app-sandbox\s*<\/key>\s*<true\s*\/>/u;

export function validateMacosEntitlements(contents) {
  if (contents === undefined) {
    return;
  }
  if (appSandboxEntitlement.test(contents)) {
    throw new Error(
      "macOS entitlements must not enable com.apple.security.app-sandbox while the app bundles the agentmetry command-line sidecar",
    );
  }
}
