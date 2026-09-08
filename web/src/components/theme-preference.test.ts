import { afterEach, describe, expect, it, vi } from "vitest";
import {
  applyThemePreference, effectiveTheme, getThemePreference, initializeThemePreference, readThemePreference, setThemePreference,
  THEME_STORAGE_KEY,
} from "./theme-preference";

const storage = (saved: string | null = null) => ({
  getItem: vi.fn(() => saved),
  setItem: vi.fn(),
});
const media = (matches = false) => ({
  matches,
  addEventListener: vi.fn(),
  removeEventListener: vi.fn(),
});

afterEach(() => {
  delete document.documentElement.dataset.theme;
  vi.restoreAllMocks();
});

describe("theme preference", () => {
  it("uses system preference by default and resolves it to the OS theme", () => {
    expect(readThemePreference(storage())).toBe("system");
    expect(effectiveTheme("system", { matches: true })).toBe("dark");
    expect(effectiveTheme("system", { matches: false })).toBe("light");
  });

  it("persists the selected preference and applies the effective theme", () => {
    const saved = storage();
    setThemePreference("dark", saved, media(false));
    expect(saved.setItem).toHaveBeenCalledWith(THEME_STORAGE_KEY, "dark");
    expect(document.documentElement.dataset.theme).toBe("dark");
  });

  it("ignores invalid values and survives storage failures", () => {
    expect(readThemePreference(storage("purple"))).toBe("system");
    const broken = { getItem: () => { throw new Error("blocked"); }, setItem: () => { throw new Error("blocked"); } };
    expect(() => initializeThemePreference(broken, media(true))).not.toThrow();
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(() => setThemePreference("light", broken, media(true))).not.toThrow();
    expect(document.documentElement.dataset.theme).toBe("light");
    expect(getThemePreference()).toBe("light");
  });

  it("applies a direct theme choice without depending on the OS", () => {
    expect(applyThemePreference("light", { matches: true })).toBe("light");
    expect(document.documentElement.dataset.theme).toBe("light");
  });

  it("keeps system mode synchronized after the settings component is gone", () => {
    let changed: (() => void) | undefined;
    const osMedia = { matches: false, addEventListener: vi.fn((_type: string, listener: () => void) => { changed = listener; }), removeEventListener: vi.fn() };
    setThemePreference("system", storage(), osMedia);
    initializeThemePreference(storage(), osMedia);
    const removalsBeforeChange = osMedia.removeEventListener.mock.calls.length;
    expect(document.documentElement.dataset.theme).toBe("light");
    osMedia.matches = true;
    changed?.();
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(osMedia.removeEventListener).toHaveBeenCalledTimes(removalsBeforeChange);
  });

  it("applies both explicit OS-independent directions", () => {
    expect(applyThemePreference("dark", { matches: false })).toBe("dark");
    expect(applyThemePreference("light", { matches: true })).toBe("light");
    expect(document.documentElement.dataset.theme).toBe("light");
  });

  it("does not reread storage after a preference was selected", () => {
    const saved = storage("system");
    setThemePreference("system", saved, media(false));
    setThemePreference("dark", { getItem: vi.fn(() => "system"), setItem: vi.fn(() => { throw new Error("blocked"); }) }, media(false));
    expect(initializeThemePreference({ getItem: vi.fn(() => "system"), setItem: vi.fn() }, media(false))).toBe("dark");
    expect(getThemePreference()).toBe("dark");
  });
});
