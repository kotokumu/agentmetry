import { msg, str } from "@lit/localize";
import type { CostSummary } from "../model/cost";
import { localization } from "../localization/localization";

export const formatCostSummary = (summary?: CostSummary): string => {
  if (summary?.amountMicroUsd === null || summary?.amountMicroUsd === undefined) return "—";
  return formatMicroUSD(summary.amountMicroUsd);
};

export const formatMicroUSD = (amount: bigint): string => {
  const sign = amount < 0n ? "-" : "";
  const absolute = amount < 0n ? -amount : amount;
  const whole = localization.number(absolute / 1_000_000n);
  let fraction = (absolute % 1_000_000n).toString().padStart(6, "0");
  while (fraction.length > 2 && fraction.endsWith("0")) fraction = fraction.slice(0, -1);
  return `${sign}$${whole}.${fraction}`;
};

export const costCoverageHint = (summary?: CostSummary): string => {
  if (!summary || summary.coverage === "unknown" || summary.coverage === "unavailable") return msg("Cost coverage unavailable", { id: "cost.coverageUnavailable" });
  const coverage = summary.coverage === "complete" ? msg("Complete", { id: "cost.complete" }) : msg("Partial", { id: "cost.partial" });
  const basis = summary.basis === "provider_reported" ? msg("Provider estimate", { id: "cost.providerEstimate" })
    : summary.basis === "rate_card_estimate" ? msg("API-equivalent estimate", { id: "cost.apiEstimate" })
      : summary.basis === "mixed" ? msg("Mixed estimates", { id: "cost.mixedEstimate" }) : msg("Estimate unavailable", { id: "cost.estimateUnavailable" });
  return msg(str`${coverage} · ${summary.pricedCalls}/${summary.eligibleCalls} calls · ${basis}`, { id: "cost.coverageHint" });
};
