export type ThemePreference = "system" | "light" | "dark";
export type EffectiveTheme = Exclude<ThemePreference, "system">;
export const THEME_STORAGE_KEY = "agentmetry.theme";
export const THEME_PREFERENCE_EVENT = "agentmetry-theme-preference-changed";

type PreferenceStorage = Pick<Storage, "getItem" | "setItem">;
type MediaQuery = Pick<MediaQueryList, "matches" | "addEventListener" | "removeEventListener">;
let stopGlobalSystemListener: (() => void) | undefined;
let currentPreference: ThemePreference = "system";
let initialized = false;

const browserStorage = (): PreferenceStorage | undefined => {
  try { return window.localStorage; } catch { return undefined; }
};

const browserMedia = (): MediaQuery | undefined => {
  try { return window.matchMedia("(prefers-color-scheme: dark)"); } catch { return undefined; }
};

export const validThemePreference = (value: unknown): value is ThemePreference =>
  value === "system" || value === "light" || value === "dark";

export const effectiveTheme = (preference: ThemePreference, media: Pick<MediaQueryList, "matches"> | undefined = browserMedia()): EffectiveTheme =>
  preference === "system" ? (media?.matches ? "dark" : "light") : preference;

export const readThemePreference = (storage: PreferenceStorage | undefined = browserStorage()): ThemePreference => {
  try {
    const saved = storage?.getItem(THEME_STORAGE_KEY);
    return validThemePreference(saved) ? saved : "system";
  } catch { return "system"; }
};

export const applyThemePreference = (preference: ThemePreference, media: Pick<MediaQueryList, "matches"> | undefined = browserMedia()) => {
  const theme = effectiveTheme(preference, media);
  try { document.documentElement.dataset.theme = theme; } catch { /* DOM metadata is best effort. */ }
  try {
    document.querySelector<HTMLMetaElement>('meta[name="theme-color"]')?.setAttribute("content", theme === "light" ? "#f5f7f9" : "#070a0f");
  } catch { /* Browser chrome metadata is best effort. */ }
  return theme;
};

export const initializeThemePreference = (
  storage: PreferenceStorage | undefined = browserStorage(),
  media: MediaQuery | undefined = browserMedia(),
) => {
  if (!initialized) {
    currentPreference = readThemePreference(storage);
    initialized = true;
  }
  applyThemePreference(currentPreference, media);
  configureGlobalSystemListener(currentPreference, media);
  return currentPreference;
};

export const getThemePreference = (): ThemePreference => currentPreference;

export const setThemePreference = (
  preference: ThemePreference,
  storage: PreferenceStorage | undefined = browserStorage(),
  media: MediaQuery | undefined = browserMedia(),
) => {
  currentPreference = preference;
  initialized = true;
  applyThemePreference(preference, media);
  configureGlobalSystemListener(preference, media);
  try { storage?.setItem(THEME_STORAGE_KEY, preference); } catch { /* Preference persistence is optional. */ }
  window.dispatchEvent(new CustomEvent(THEME_PREFERENCE_EVENT, { detail: { preference } }));
};

const configureGlobalSystemListener = (preference: ThemePreference, media: MediaQuery | undefined) => {
  stopGlobalSystemListener?.();
  stopGlobalSystemListener = undefined;
  if (preference !== "system" || !media) return;
  const onChange = () => {
    applyThemePreference("system", media);
    window.dispatchEvent(new CustomEvent(THEME_PREFERENCE_EVENT, { detail: { preference: "system" } }));
  };
  media.addEventListener("change", onChange);
  stopGlobalSystemListener = () => media.removeEventListener("change", onChange);
};
