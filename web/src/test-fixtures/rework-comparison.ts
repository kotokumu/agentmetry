import { create } from "@bufbuild/protobuf";
import { CompareReworkResponseSchema } from "../gen/agentmetry/v1/agentmetry_pb";

// Explicit comparison oracle shared by transport-mapping and presentation tests.
const harness = (current: boolean) => ({
  counts: { eligibleRecords: 4n, reportedRecords: 4n, unreportedRecords: 0n, invalidRecords: 0n, distinctIdentities: 1n },
  classification: { case: "uniform" as const, value: { identity: { scope: "project-7f2a", fingerprint: current ? "sha256:dfbc1de58f3b905c7b0c0fd79361699336b5f9da617b1db8f35c76673f95b29d" : "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db", label: current ? "AGENTS v2" : "AGENTS v1" } } },
});
export const comparisonWire = () => create(CompareReworkResponseSchema, {
  status: "ready",
  baseline: { sourceId: "codex", sessionId: "before", harnessContext: harness(false), projectionCoverage: "complete", coverage: { activityCoverage: "observed_projection_complete" } },
  current: { sourceId: "codex", sessionId: "after", harnessContext: harness(true), projectionCoverage: "partial", coverage: { activityCoverage: "partial_page" } },
  rows: [
    ["initial_validation_success_proxy", "percent", 1, 4, 25, 3, 4, 75, 50],
    ["rework_token_share", "percent", 400, 1000, 40, 200, 1000, 20, -20],
    ["retry_cycle_effort_share", "percent", 400, 1000, 40, 100, 1000, 10, -30],
    ["tool_failure_rate", "percent", 2, 4, 50, 1, 4, 25, -25],
    ["recurring_loops_per_100_validations", "per100", 2, 4, 50, 1, 5, 20, -30],
  ].map(([id, unit, bn, bd, bv, cn, cd, cv, delta]) => ({
    id: String(id), unit: String(unit), availability: "comparable", delta: Number(delta),
    baseline: { availability: "available", numerator: Number(bn), denominator: Number(bd), value: Number(bv) },
    current: { availability: "available", numerator: Number(cn), denominator: Number(cd), value: Number(cv) },
  })),
});
