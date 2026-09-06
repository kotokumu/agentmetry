import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { ListSessionsResponseSchema, SessionListView, SessionRole } from "../gen/agentmetry/v1/agentmetry_pb";
import { mapSessionListResponse } from "./agentmetry-client";

describe("telemetry session catalog mapping", () => {
  it("keeps legacy root rows as IDs without inventing roles", () => {
    const response = create(ListSessionsResponseSchema, { sessions: [{ id: "native", sourceId: "claude" }] });
    const result = mapSessionListResponse(response, "roots");
    expect(result.sessions[0]).toMatchObject({ id: "native", sourceId: "claude" });
    expect(result.sessions[0].catalog).toBeUndefined();
    expect(result.nextPageToken).toBe("");
  });
  it.each([SessionListView.UNSPECIFIED, SessionListView.ROOTS, 99])("rejects unacknowledged all (%s)", (appliedView) => {
    expect(() => mapSessionListResponse(create(ListSessionsResponseSchema, { appliedView }), "all")).toThrow("Session list unavailable");
  });
  it("maps a child and its opaque next token without changing the label", () => {
    const response = create(ListSessionsResponseSchema, { appliedView: SessionListView.ALL, page: { hasMore: true, nextPageToken: "opaque" }, sessions: [{ id: "child", sourceId: "codex", catalog: { role: SessionRole.CHILD, rootSessionId: "root", parentSessionId: "parent" } }] });
    expect(mapSessionListResponse(response, "all")).toMatchObject({ nextPageToken: "opaque", sessions: [{ id: "child", catalog: { role: "child", rootSessionId: "root", parentSessionId: "parent" } }] });
  });
  it.each([
    undefined,
    { role: SessionRole.ROOT, rootSessionId: "other" },
    { role: SessionRole.CHILD, rootSessionId: "root", parentSessionId: "child" },
    { role: SessionRole.CHILD, rootSessionId: "child", parentSessionId: "parent" },
    { role: 99, rootSessionId: "root", parentSessionId: "parent" },
  ])("rejects invalid all relationships", (catalog) => {
    const response = create(ListSessionsResponseSchema, { appliedView: SessionListView.ALL, sessions: [{ id: "child", sourceId: "codex", catalog }] });
    expect(() => mapSessionListResponse(response, "all")).toThrow("Session list unavailable");
  });
  it("does not accept a contradictory view or child row as roots", () => {
    expect(() => mapSessionListResponse(create(ListSessionsResponseSchema, { appliedView: SessionListView.ALL }), "roots")).toThrow();
    const result = mapSessionListResponse(create(ListSessionsResponseSchema, { appliedView: SessionListView.ROOTS, sessions: [{ id: "child", sourceId: "codex", catalog: { role: SessionRole.CHILD, rootSessionId: "root", parentSessionId: "root" } }] }), "roots");
    expect(result.sessions[0].catalog).toBeUndefined();
  });
});
