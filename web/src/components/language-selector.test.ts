import { afterEach, describe, expect, it } from "vitest";
import { initializeLocale, localization } from "../localization/localization";
import "./language-selector";
import type { LanguageSelector } from "./language-selector";

afterEach(async () => {
  await localization.select("en");
  document.body.replaceChildren();
});

describe("LanguageSelector", () => {
  it("shows the system Japanese locale on its first render", async () => {
    await initializeLocale({
      storage: { getItem: () => null, setItem: () => undefined },
      languages: ["ja-JP"],
    });
    const selector = document.createElement("am-language-selector") as LanguageSelector;

    document.body.append(selector);
    await selector.updateComplete;

    const select = selector.shadowRoot?.querySelector("select");
    expect(selector.shadowRoot?.textContent).toContain("言語");
    expect(select?.value).toBe("ja");
  });

  it("renders registered languages and switches the active locale", async () => {
    await localization.select("en");
    const selector = document.createElement("am-language-selector") as LanguageSelector;
    document.body.append(selector);
    await selector.updateComplete;

    const select = selector.shadowRoot?.querySelector("select");
    expect(selector.shadowRoot?.textContent).toContain("Language");
    expect(Array.from(select?.options ?? []).map(({ value, text }) => [value, text])).toEqual([
      ["en", "English"],
      ["ja", "日本語"],
    ]);

    select!.value = "ja";
    select!.dispatchEvent(new Event("change"));
    await localization.whenReady();
    await selector.updateComplete;

    expect(localization.locale).toBe("ja");
    expect(selector.shadowRoot?.textContent).toContain("言語");
  });
});
