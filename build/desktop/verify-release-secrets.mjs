import { missingReleaseSecrets } from "./release-secrets.mjs";

const missing = missingReleaseSecrets(process.env);
if (missing.length > 0) {
  throw new Error(`missing GitHub release secrets: ${missing.join(", ")}`);
}

console.log("release signing and notarization secrets are configured");
