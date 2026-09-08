export type CostBasis = "provider_reported" | "rate_card_estimate" | "mixed" | "unavailable" | "unknown";
export type CostCoverage = "complete" | "partial" | "unavailable" | "unknown";
export type CostReasonCount = Readonly<{ reason: string; count: bigint }>;
export type CostSummary = Readonly<{
  amountMicroUsd: bigint | null;
  basis: CostBasis;
  coverage: CostCoverage;
  eligibleCalls: bigint;
  pricedCalls: bigint;
  unpricedReasons: readonly CostReasonCount[];
  aggregateError?: string;
}>;

export type ModelCallCost = Readonly<{
  callId: string; identityBasis: string; basis: CostBasis; amountMicroUsd: bigint | null;
  primaryReason?: string; rateEntryId?: string;
}>;

export type ModelCallRef = Readonly<{ callId: string; identityBasis: string; evidenceRole: string }>;
