import { describe, expect, it } from "vitest";
import { costCoverageHint, formatCostSummary } from "./cost";

describe("cost presentation", () => {
	it("formats micro-USD without converting the canonical bigint to number", () => {
		expect(formatCostSummary({ amountMicroUsd: 3345n, basis: "rate_card_estimate", coverage: "complete", eligibleCalls: 1n, pricedCalls: 1n, unpricedReasons: [] })).toBe("$0.003345");
		expect(formatCostSummary({ amountMicroUsd: 1_200_000n, basis: "rate_card_estimate", coverage: "complete", eligibleCalls: 1n, pricedCalls: 1n, unpricedReasons: [] })).toBe("$1.20");
		expect(formatCostSummary({ amountMicroUsd: 1_234_500_000n, basis: "rate_card_estimate", coverage: "complete", eligibleCalls: 1n, pricedCalls: 1n, unpricedReasons: [] })).toBe("$1,234.50");
		expect(formatCostSummary()).toBe("—");
	});

  it("labels partial subtotals with call coverage", () => {
		expect(costCoverageHint({ amountMicroUsd: 12n, basis: "mixed", coverage: "partial", eligibleCalls: 3n, pricedCalls: 2n, unpricedReasons: [{ reason: "rate_not_found", count: 1n }] })).toBe("Partial · 2/3 calls · Mixed estimates");
	});
});
