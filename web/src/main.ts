import { initializeLocale } from "./localization/localization";

await initializeLocale();
await import("./app/agentmetry-app");
