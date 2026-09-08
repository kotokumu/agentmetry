import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import { ListSessionsResponseSchema, SessionListView, SessionRole } from "../gen/agentmetry/v1/agentmetry_pb";
import { mapSessionListResponse } from "./agentmetry-client";

describe("telemetry session catalog mapping", () => {
  it.each([true, false])("maps observed Codex names without changing identity (known time: %s)", (knownTime) => {
    const observedAt = knownTime ? timestampFromDate(new Date("2026-09-09T01:02:03Z")) : undefined;
    const response = create(ListSessionsResponseSchema, { appliedView: SessionListView.ROOTS, sessions: [{
      id: "native", sourceId: "codex", catalog: { role: SessionRole.ROOT, rootSessionId: "native",
        name: { text: " <b>Observed</b> ", origin: "codex_app.list_threads", observedAt } },
    }] });
    expect(mapSessionListResponse(response, "roots").sessions[0]).toMatchObject({
      id: "native", sourceId: "codex", catalog: { name: { text: " <b>Observed</b> ", origin: "codex_app.list_threads",
        ...(knownTime ? { observedAt: "2026-09-09T01:02:03.000Z" } : {}) } },
    });
  });
  it.each(["claude", "unknown"])("does not transfer Codex names to %s", (sourceId) => {
    const response = create(ListSessionsResponseSchema, { appliedView: SessionListView.ALL, sessions: [{
      id: "child", sourceId, catalog: { role: SessionRole.CHILD, rootSessionId: "root", parentSessionId: "parent",
        name: { text: "Wrong source", origin: "codex_app.list_threads" } },
    }] });
    expect(mapSessionListResponse(response, "all").sessions[0].catalog).toEqual({ role: "child", rootSessionId: "root", parentSessionId: "parent" });
  });
  it.each([true, false])("maps a generated name without changing identity (known time: %s)", (knownTime) => {
    const observedAt = knownTime ? timestampFromDate(new Date("2026-09-01T12:00:00Z")) : undefined;
    const response = create(ListSessionsResponseSchema, { appliedView: SessionListView.ROOTS, sessions: [{
      id: "native", sourceId: "claude", catalog: { role: SessionRole.ROOT, rootSessionId: "native",
        name: { text: "  Fix <session> names  ", origin: "claude_code.generate_session_title", observedAt } },
    }] });
    expect(mapSessionListResponse(response, "roots").sessions[0]).toMatchObject({
      id: "native", sourceId: "claude", catalog: { role: "root", name: {
        text: "  Fix <session> names  ", origin: "claude_code.generate_session_title",
        ...(knownTime ? { observedAt: "2026-09-01T12:00:00.000Z" } : {}),
      } },
    });
    if (!knownTime) expect(mapSessionListResponse(response, "roots").sessions[0].catalog?.name?.observedAt).toBeUndefined();
  });
  it.each([
    { text: "Name", origin: "future_provider.name" },
    { text: "Name", origin: "" },
    { text: "", origin: "claude_code.generate_session_title" },
    { text: " \t\n ", origin: "claude_code.generate_session_title" },
    { text: "Name", origin: "claude_code.generate_session_title", observedAt: { seconds: 253402300800n } },
    { text: "Name", origin: "claude_code.generate_session_title", observedAt: { seconds: -62135596801n } },
    { text: "Name", origin: "claude_code.generate_session_title", observedAt: { nanos: -1 } },
    { text: "Name", origin: "claude_code.generate_session_title", observedAt: { nanos: 1000000000 } },
  ])("ignores unsupported name metadata without losing the child relationship", (name) => {
    const response = create(ListSessionsResponseSchema, { appliedView: SessionListView.ALL, sessions: [{
      id: "child", sourceId: "claude", catalog: { role: SessionRole.CHILD, rootSessionId: "root", parentSessionId: "parent", name },
    }] });
    const row = mapSessionListResponse(response, "all").sessions[0];
    expect(row.id).toBe("child");
    expect(row.catalog).toEqual({ role: "child", rootSessionId: "root", parentSessionId: "parent" });
  });
  it("does not apply a Claude generated name to a Codex session", () => {
    const response = create(ListSessionsResponseSchema, { appliedView: SessionListView.ROOTS, sessions: [{
      id: "native", sourceId: "codex", catalog: { role: SessionRole.ROOT, rootSessionId: "native",
        name: { text: "Name", origin: "claude_code.generate_session_title" } },
    }] });
    expect(mapSessionListResponse(response, "roots").sessions[0].catalog).toEqual({ role: "root", rootSessionId: "native", parentSessionId: "" });
  });
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
