import { comparisonWire } from "../test-fixtures/rework-comparison";
import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { ActivitySchema, CompareReworkResponseSchema, HarnessContextSchema, TokenUsageSchema } from "../gen/agentmetry/v1/agentmetry_pb";
import { assertSessionConditionsApplied, mapActivityContentEvidence, mapHarnessContext, mapOptionalSessionTokens, mapReworkComparison } from "./agentmetry-client";

describe("harness context API mapping", () => {
  it("preserves an absent field as server unsupported", () => {
    expect(mapHarnessContext(undefined)).toEqual({ availability: "unavailable", reason: "server_unsupported" });
  });

  it("maps a complete uniform identity", () => {
    const value = create(HarnessContextSchema, {
      counts: { eligibleRecords: 2n, reportedRecords: 2n, unreportedRecords: 0n, invalidRecords: 0n, distinctIdentities: 1n },
      classification: {
        case: "uniform",
        value: { identity: { scope: "project-7f2a", fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db", label: "AGENTS v2" } },
      },
    });

    expect(mapHarnessContext(value)).toEqual({
      availability: "available",
      state: "uniform",
      counts: { eligibleRecords: 2, reportedRecords: 2, unreportedRecords: 0, invalidRecords: 0, distinctIdentities: 1 },
      identity: { scope: "project-7f2a", fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db", label: "AGENTS v2" },
    });
  });

  it("closes impossible public states as an invalid server payload", () => {
    const missingIdentity = create(HarnessContextSchema, {
      counts: { eligibleRecords: 1n, reportedRecords: 1n, distinctIdentities: 1n },
      classification: { case: "uniform", value: {} },
    });
    const inconsistentCounts = create(HarnessContextSchema, {
      counts: { eligibleRecords: 2n, reportedRecords: 1n, unreportedRecords: 0n, invalidRecords: 0n, distinctIdentities: 1n },
      classification: { case: "incomplete", value: {} },
    });
    const unspecified = create(HarnessContextSchema, {
      counts: { eligibleRecords: 0n, reportedRecords: 0n, unreportedRecords: 0n, invalidRecords: 0n, distinctIdentities: 0n },
    });

    expect(mapHarnessContext(missingIdentity)).toEqual({ availability: "unavailable", reason: "invalid_server_payload" });
    expect(mapHarnessContext(inconsistentCounts)).toEqual({ availability: "unavailable", reason: "invalid_server_payload" });
    expect(mapHarnessContext(unspecified)).toEqual({ availability: "unavailable", reason: "invalid_server_payload" });
  });
});

describe("optional session token API mapping", () => {
  const unsupported = { availability: "unavailable", reason: "server_unsupported" } as const;
  const reported = { availability: "available", state: "unreported", counts: { eligibleRecords: 1, reportedRecords: 0, unreportedRecords: 1, invalidRecords: 0, distinctIdentities: 0 } } as const;

  it("falls back only when both additive fields identify an old server", () => {
    expect(mapOptionalSessionTokens(undefined, unsupported)).toBeUndefined();
    expect(mapOptionalSessionTokens(undefined, reported)).toEqual({
      input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null,
    });
    expect(mapOptionalSessionTokens(create(TokenUsageSchema), unsupported)).toEqual({
      input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null,
    });
  });
});


describe("shared comparison response", () => {
  it("preserves every metric operand, unit, value and delta with separate coverage", () => {
    const result = mapReworkComparison(comparisonWire());
    expect(result).toMatchObject({ status: "ready", baseline: { sessionId: "before", projectionCoverage: "complete" }, current: { sessionId: "after", projectionCoverage: "partial" } });
    if (result.status !== "ready") throw new Error("expected ready");
    expect(result.rows.map((row) => [row.id, row.unit, row.baseline.numerator, row.baseline.denominator, row.baseline.availability === "available" ? row.baseline.displayValue : null, row.current.numerator, row.current.denominator, row.current.availability === "available" ? row.current.displayValue : null, row.availability === "comparable" ? row.delta : null])).toEqual([
      ["initial_validation_success_proxy", "percent", 1, 4, 25, 3, 4, 75, 50],
      ["rework_token_share", "percent", 400, 1000, 40, 200, 1000, 20, -20],
      ["retry_cycle_effort_share", "percent", 400, 1000, 40, 100, 1000, 10, -30],
      ["tool_failure_rate", "percent", 2, 4, 50, 1, 4, 25, -25],
      ["recurring_loops_per_100_validations", "per100", 2, 4, 50, 1, 5, 20, -30],
    ]);
    expect(result.warnings).toEqual(["Current evidence is a partial retained projection."]);
  });

  it("retains a raw tiny delta while classifying the one-decimal display as unchanged", () => {
    const wire = comparisonWire();
    wire.rows[0].baseline!.value = 50;
    wire.rows[0].current!.value = 50.04;
    wire.rows[0].delta = 0.04;
    const result = mapReworkComparison(wire);
    if (result.status !== "ready") throw new Error("expected ready");
    expect(result.rows[0]).toMatchObject({ delta: 0.04, direction: "unchanged" });
  });

  it("keeps unavailable operands and reasons without fabricating zero", () => {
    const wire = comparisonWire();
    wire.rows[0].availability = "unavailable";
    wire.rows[0].delta = undefined;
    wire.rows[0].baseline = { ...wire.rows[0].baseline!, availability: "unavailable", numerator: undefined, denominator: 0, value: undefined, reason: "No eligible validation identities" };
    const result = mapReworkComparison(wire);
    if (result.status !== "ready") throw new Error("expected ready");
    expect(result.rows[0]).toMatchObject({ availability: "unavailable", baseline: { numerator: null, denominator: 0, reason: "No eligible validation identities" } });
  });

  it("rejects empty, incomplete or unknown comparison payloads", () => {
    expect(() => mapReworkComparison(create(CompareReworkResponseSchema))).toThrow("comparison response");
    const wire = comparisonWire(); wire.rows.pop();
    expect(() => mapReworkComparison(wire)).toThrow("comparison response");
    const unknown = comparisonWire(); unknown.rows[0].id = "new_metric";
    expect(() => mapReworkComparison(unknown)).toThrow("comparison response");
  });
});

describe("structured conversation condition acknowledgement", () => {
  it("accepts old servers only for empty conditions and rejects silently ignored conditions", () => {
    expect(() => assertSessionConditionsApplied({}, undefined)).not.toThrow();
    expect(() => assertSessionConditionsApplied({ observedFailure: true, minDurationMs: 0, tool: "Read" }, undefined)).toThrow(/support/);
    expect(() => assertSessionConditionsApplied({ minDurationMs: 0 }, { minDurationMs: undefined })).toThrow(/support/);
    expect(() => assertSessionConditionsApplied({ model: "a", tool: "Read" }, { model: "a", tool: "Write" })).toThrow(/support/);
    expect(() => assertSessionConditionsApplied({ observedFailure: true, minDurationMs: 0, tool: "Read" }, { observedFailure: true, minDurationMs: 0, tool: "Read", model: "" })).not.toThrow();
  });
});


describe("activity content evidence mapping", () => {
  it("keeps old server body semantics unknown", () => {
    expect(mapActivityContentEvidence(create(ActivitySchema, {id:"a", source:"claude", signal:"log", content:"AGENTS.md"}))).toEqual({source:"claude",activityId:"a",signal:"log",kind:"unknown",evidence:"unknown",availability:"available",fields:[],truncated:false});
  });
  it("keeps old server body absence separate from meaning", () => {
    expect(mapActivityContentEvidence(create(ActivitySchema, {id:"a", source:"claude", signal:"trace"}))).toEqual({source:"claude",activityId:"a",signal:"trace",kind:"unknown",evidence:"unknown",availability:"not_reported",fields:[],truncated:false});
  });
  it("preserves reference evidence and missing body independently", () => {
    expect(mapActivityContentEvidence(create(ActivitySchema, {id:"a",source:"claude",signal:"log",content:"file:///request",contentEvidence:{source:"claude",activityId:"a",signal:"log",kind:"reference",evidence:"reference",availability:"not_reported",fields:["body_ref"]}}))).toEqual({source:"claude",activityId:"a",signal:"log",kind:"reference",evidence:"reference",availability:"not_reported",fields:["body_ref"],truncated:false});
  });
  it("preserves readable output alongside encrypted input evidence", () => {
    expect(mapActivityContentEvidence(create(ActivitySchema, {id:"a",source:"codex",signal:"log",contentEvidence:{source:"codex",activityId:"a",signal:"log",kind:"tool_output",evidence:"read_output",availability:"available",fields:["arguments.message","output"],redactionReason:"encrypted_input",truncated:true}}))).toEqual({source:"codex",activityId:"a",signal:"log",kind:"tool_output",evidence:"read_output",availability:"available",fields:["arguments.message","output"],redactionReason:"encrypted_input",truncated:true});
  });
  it("does not attach another activity's semantics", () => {
    expect(mapActivityContentEvidence(create(ActivitySchema, {id:"a",source:"codex",signal:"log",contentEvidence:{source:"claude",activityId:"b",signal:"log",kind:"model_input",evidence:"explicit_model_input",availability:"available",fields:["body"]}}))).toEqual({source:"codex",activityId:"a",signal:"log",kind:"unknown",evidence:"unknown",availability:"not_reported",fields:[],truncated:false});
  });
  it("keeps transport suppression while unknown future semantics fall back", () => {
    expect(mapActivityContentEvidence(create(ActivitySchema, {id:"a",source:"codex",signal:"log",contentEvidence:{source:"codex",activityId:"a",signal:"log",kind:"future",evidence:"future",availability:"not_returned",fields:["arbitrary payload"]}}))).toEqual({source:"codex",activityId:"a",signal:"log",kind:"unknown",evidence:"unknown",availability:"not_returned",fields:[],truncated:false});
  });
});
