import { configureLocalization } from "@lit/localize";
import type { LocaleModule } from "@lit/localize";
import { allLocales, sourceLocale, targetLocales } from "./generated/locale-codes";
import { sourceMessages, type MessageKey, type MessageParameters } from "./messages";

export const LOCALE_STORAGE_KEY = "agentmetry.locale";

export type LocaleCode = typeof allLocales[number];
type TargetLocale = typeof targetLocales[number];

const supportedLocaleCodes = new Set<string>(allLocales);
const localeNames: Readonly<Record<LocaleCode, string>> = { en: "English", ja: "日本語" };
const intlLocales: Readonly<Record<LocaleCode, string>> = { en: "en-US", ja: "ja-JP" };
const localeLoaders: Readonly<Record<TargetLocale, () => Promise<LocaleModule>>> = {
  ja: () => import("./generated/locales/ja.js"),
};

export const supportedLocales: readonly Readonly<{ code: LocaleCode; name: string }>[] = allLocales.map(
  (code) => ({ code, name: localeNames[code] }),
);

const { getLocale, setLocale: setLibraryLocale } = configureLocalization({
  sourceLocale,
  targetLocales,
  loadLocale: async (locale) => {
    const load = localeLoaders[locale as TargetLocale];
    if (!load) throw new Error(`Unsupported locale module: ${locale}`);
    return load();
  },
});

const matchedLocale = (value: unknown): LocaleCode | undefined => {
  if (typeof value !== "string") return undefined;
  const normalized = value.trim().toLowerCase().replaceAll("_", "-");
  if (!normalized) return undefined;
  return allLocales.find((locale) => normalized === locale || normalized.startsWith(`${locale}-`));
};

export function resolveLocale(saved: unknown, languages: readonly string[]): LocaleCode {
  const savedLocale = matchedLocale(saved);
  if (savedLocale) return savedLocale;
  for (const language of languages) {
    const locale = matchedLocale(language);
    if (locale) return locale;
  }
  return sourceLocale;
}

type LocaleStorage = Pick<Storage, "getItem" | "setItem">;
type InitializationOptions = Readonly<{ storage?: LocaleStorage; languages?: readonly string[] }>;

const browserStorage = (): LocaleStorage | undefined => {
  try { return window.localStorage; } catch { return undefined; }
};

const browserLanguages = (): readonly string[] => {
  try { return navigator.languages?.length ? navigator.languages : [navigator.language]; } catch { return []; }
};

let localeReady: Promise<void> = Promise.resolve();

const activateLocale = (locale: LocaleCode): Promise<void> => {
  localeReady = setLibraryLocale(locale);
  return localeReady;
};

export async function initializeLocale(options: InitializationOptions = {}): Promise<void> {
  const storage = options.storage ?? browserStorage();
  let saved: unknown;
  try { saved = storage?.getItem(LOCALE_STORAGE_KEY); } catch { saved = undefined; }
  const locale = resolveLocale(saved, options.languages ?? browserLanguages());
  try { await activateLocale(locale); } catch { /* The source locale remains active when loading fails. */ }
}

export async function selectLocale(locale: string, storage: LocaleStorage | undefined = browserStorage()): Promise<void> {
  if (!supportedLocaleCodes.has(locale)) return;
  await activateLocale(locale as LocaleCode);
  try { storage?.setItem(LOCALE_STORAGE_KEY, locale); } catch { /* Preference persistence is optional. */ }
}

export const localization = {
  get locale(): LocaleCode { return getLocale() as LocaleCode; },

  select(locale: string): Promise<void> { return selectLocale(locale); },

  whenReady(): Promise<void> { return localeReady; },

  t(key: MessageKey, parameters: MessageParameters = {}): string {
    return sourceMessages[key](parameters);
  },

  number(value: number | bigint, options?: Intl.NumberFormatOptions): string {
    return new Intl.NumberFormat(intlLocales[this.locale], options).format(value);
  },

  dateTime(value: Date | number, options?: Intl.DateTimeFormatOptions): string {
    return new Intl.DateTimeFormat(intlLocales[this.locale], options).format(value);
  },
};
