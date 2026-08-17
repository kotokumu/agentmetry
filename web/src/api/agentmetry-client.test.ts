import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { HarnessContextSchema, TokenUsageSchema } from "../gen/agentmetry/v1/agentmetry_pb";
import { mapHarnessContext, mapOptionalSessionTokens } from "./agentmetry-client";

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
