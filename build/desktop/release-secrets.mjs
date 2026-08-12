export const REQUIRED_RELEASE_SECRETS = Object.freeze([
  "TAURI_SIGNING_PRIVATE_KEY",
  "APPLE_CERTIFICATE",
  "APPLE_CERTIFICATE_PASSWORD",
  "KEYCHAIN_PASSWORD",
  "APPLE_ID",
  "APPLE_PASSWORD",
  "APPLE_TEAM_ID",
]);

export function missingReleaseSecrets(environment) {
  return REQUIRED_RELEASE_SECRETS.filter((name) => {
    const value = environment[name];
    return typeof value !== "string" || value.trim() === "";
  });
}
