import { afterEach, describe, expect, it } from "vitest";
import { html } from "lit";
import { LocalizedElement } from "./localized-element";
import {
  LOCALE_STORAGE_KEY,
  initializeLocale,
  localization,
  resolveLocale,
  selectLocale,
} from "./localization";

class MemoryStorage implements Pick<Storage, "getItem" | "setItem"> {
  readonly values = new Map<string, string>();
  getItem(key: string) { return this.values.get(key) ?? null; }
  setItem(key: string, value: string) { this.values.set(key, value); }
}

class TestLocalizedCopy extends LocalizedElement {
  render() { return html`<span>${localization.t("language.label")}</span>`; }
}

if (!customElements.get("test-localized-copy")) customElements.define("test-localized-copy", TestLocalizedCopy);

afterEach(async () => {
  await localization.select("en");
  document.body.replaceChildren();
});

describe("locale resolution", () => {
  it.each([
    ["ja", ["en-US"], "ja"],
    ["unsupported", ["ja-JP", "en-US"], "ja"],
    [null, ["JA-jp"], "ja"],
    [undefined, ["fr-FR"], "en"],
    ["", [], "en"],
  ] as const)("resolves saved=%s and languages=%j to %s", (saved, languages, expected) => {
    expect(resolveLocale(saved, languages)).toBe(expected);
  });
});

describe("library-backed locale bootstrap", () => {
  it("restores the saved locale and formats with the library active locale", async () => {
    const storage = new MemoryStorage();
    storage.setItem(LOCALE_STORAGE_KEY, "ja");

    await initializeLocale({ storage, languages: ["en-US"] });

    expect(localization.locale).toBe("ja");
    expect(localization.t("language.label")).toBe("言語");
    expect(localization.number(1234.5, { maximumFractionDigits: 1 })).toBe("1,234.5");
    expect(localization.dateTime(new Date("2026-01-02T00:00:00Z"), {
      year: "numeric", month: "short", day: "numeric", timeZone: "UTC",
    })).toContain("2026年1月2日");
  });

  it("persists an explicit selection after the library activates it", async () => {
    const storage = new MemoryStorage();

    await selectLocale("ja", storage);

    expect(localization.locale).toBe("ja");
    expect(storage.getItem(LOCALE_STORAGE_KEY)).toBe("ja");
  });

  it("keeps i18n usable when preference reads and writes throw", async () => {
    const storage = {
      getItem: () => { throw new Error("blocked"); },
      setItem: () => { throw new Error("blocked"); },
    };

    await initializeLocale({ storage, languages: ["ja-JP"] });
    await expect(selectLocale("en", storage)).resolves.toBeUndefined();

    expect(localization.t("language.label")).toBe("Language");
  });
});

describe("Lit localization integration", () => {
  it("rerenders an already-connected shadow root after a locale change", async () => {
    await localization.select("en");
    const element = document.createElement("test-localized-copy") as TestLocalizedCopy;
    document.body.append(element);
    await element.updateComplete;
    expect(element.shadowRoot?.textContent).toContain("Language");

    await localization.select("ja");
    await element.updateComplete;

    expect(element.shadowRoot?.textContent).toContain("言語");
  });
});
