import { describe, expect, it } from "vitest";
import { parseInvestigationFilters, conditionParameters, filtersFromParameters, sessionConditions } from "./investigation-conditions";

describe("investigation conditions", () => {
  it("round trips all AND conditions including reported zero and relative time", () => {
    const value = { range: "24h", sourceId: "codex", search: "failure", observedFailure: true, minDurationMs: 0, maxDurationMs: 1200, model: "model-a", tool: "read" } as const;
    expect(filtersFromParameters(conditionParameters(value))).toEqual(value);
    expect(sessionConditions(value)).toEqual({ observedFailure: true, minDurationMs: 0, maxDurationMs: 1200, model: "model-a", tool: "read" });
  });
  it.each([{ minDurationMs: -1 }, { minDurationMs: 10, maxDurationMs: 1 }, { maxDurationMs: Infinity }, { range: "forever" }, { unknownCondition: "ignored?" }, { observedFailure: "true" }])("rejects invalid or unknown conditions %j", (invalid) => {
    expect(() => parseInvestigationFilters({ range: "24h", sourceId: "", search: "", ...invalid })).toThrow();
  });
  it("does not silently drop invalid numeric URL fields", () => {
    expect(() => filtersFromParameters(new URLSearchParams("minMs=banana"))).toThrow();
    expect(() => filtersFromParameters(new URLSearchParams("failure=maybe"))).toThrow();
  });
});
