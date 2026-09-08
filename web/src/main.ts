import { initializeLocale } from "./localization/localization";
import { initializeThemePreference } from "./components/theme-preference";

await initializeLocale();
initializeThemePreference();
await import("./app/agentmetry-app");
