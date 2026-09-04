import { afterEach, describe, expect, it } from "vitest";
import { localization } from "../localization/localization";
import "./activity-table";
import "./investigation-filter";
import "./mcp-connection";
import "./rework-comparison";
import "./rework-summary";
import "./session-filter";
import "./session-list";
import "./trace-overview";

afterEach(async () => {
  await localization.select("en");
  document.body.replaceChildren();
});

describe("localized feature components", () => {
  it("rerenders connected nested feature states in Japanese", async () => {
    await localization.select("en");
    const tags = [
      "am-session-filter",
      "am-session-list",
      "am-investigation-filter",
      "am-activity-table",
      "am-trace-overview",
      "am-rework-summary",
      "am-rework-comparison",
      "am-mcp-connection",
    ] as const;
    const elements = tags.map((tag) => document.createElement(tag));
    document.body.append(...elements);
    await Promise.all(elements.map((element) => element.updateComplete));

    await localization.select("ja");
    await Promise.all(elements.map((element) => element.updateComplete));
    const text = elements.map((element) => element.shadowRoot?.textContent ?? "").join(" ");

    expect(text).toContain("すべてのソース");
    expect(text).toContain("会話はまだありません");
    expect(text).toContain("調査フィルター");
    expect(text).toContain("アクティビティ詳細");
    expect(text).toContain("トレース概要を読み込み中");
    expect(text).toContain("手戻り分析を待機中");
    expect(text).toContain("前後の診断比較");
    expect(text).toContain("詳細");
  });
});
