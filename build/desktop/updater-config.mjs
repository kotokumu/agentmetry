const releaseTagPattern = /^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/;

export function releaseVersion(tag, appVersion) {
  if (!releaseTagPattern.test(tag)) {
    throw new Error(`release tag ${tag} must match v<semantic-version>`);
  }

  const tagVersion = tag.slice(1);
  if (tagVersion !== appVersion) {
    throw new Error(
      `tag version ${tagVersion} does not match Tauri version ${appVersion}`,
    );
  }
  return tagVersion;
}

export function validateUpdaterConfig(config) {
  if (config.bundle?.createUpdaterArtifacts !== true) {
    throw new Error("bundle.createUpdaterArtifacts must be true");
  }

  const updater = config.plugins?.updater;
  const endpoint = updater?.endpoints?.[0];
  if (typeof endpoint !== "string" || !endpoint.startsWith("https://")) {
    throw new Error("plugins.updater.endpoints must start with an HTTPS URL");
  }
  if (typeof updater?.pubkey !== "string" || updater.pubkey.trim().length < 40) {
    throw new Error("plugins.updater.pubkey must contain the updater public key");
  }

  return { endpoint, createsArtifacts: true };
}
