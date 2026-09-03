import { afterEach, describe, expect, it } from "vitest";
import { ContentEvidencePanel, readableActivityContent } from "./content-evidence";

afterEach(() => document.body.replaceChildren());

describe("received content evidence", () => {
  it("keeps a received reference visible without claiming a body or model input", async () => {
    const panel = new ContentEvidencePanel();
    panel.activityContent = "/tmp/AGENTS.md";
    panel.evidence = { source: "claude", activityId: "ref", signal: "log", kind: "reference", evidence: "reference", availability: "not_reported", fields: ["body_ref"], truncated: false };
    document.body.append(panel);
    await panel.updateComplete;
    expect(panel.shadowRoot?.textContent).toContain("Reference only");
    expect(panel.shadowRoot?.textContent).toContain("Body not reported");
    expect(panel.shadowRoot?.textContent).toContain("Model-input inclusion is unconfirmed");
    expect(panel.shadowRoot?.textContent).toContain("body_ref");
    expect(readableActivityContent(panel.evidence, panel.activityContent)).toBe("/tmp/AGENTS.md");
  });

  it("preserves readable tool output when input was encrypted and reports truncation", async () => {
    const panel = new ContentEvidencePanel();
    panel.activityContent = "Received result";
    panel.evidence = { source: "codex", activityId: "output", signal: "log", kind: "tool_output", evidence: "read_output", availability: "available", fields: ["output"], truncated: true, redactionReason: "encrypted_input" };
    document.body.append(panel);
    await panel.updateComplete;
    expect(panel.shadowRoot?.textContent).toContain("Received tool output");
    expect(panel.shadowRoot?.textContent).toContain("Input was encrypted");
    expect(panel.shadowRoot?.textContent).toContain("Received content is truncated");
    expect(readableActivityContent(panel.evidence, panel.activityContent)).toBe("Received result");
  });

  it("does not render explicit redaction markers as readable body content", async () => {
    const panel = new ContentEvidencePanel();
    panel.evidence = { source: "codex", activityId: "prompt", signal: "log", kind: "prompt", evidence: "unknown", availability: "redacted", fields: ["prompt"], truncated: false, redactionReason: "producer_redacted" };
    document.body.append(panel);
    await panel.updateComplete;
    expect(panel.shadowRoot?.textContent).toContain("Producer-redacted");
    expect(readableActivityContent(panel.evidence, "[REDACTED]")).toBe("");
  });

  it("keeps an old server's body provenance unknown", async () => {
    const panel = new ContentEvidencePanel();
    panel.activityContent = "Received body mentioning AGENTS.md";
    document.body.append(panel);
    await panel.updateComplete;
    expect(panel.shadowRoot?.textContent).toContain("Unknown");
    expect(panel.shadowRoot?.textContent).toContain("Body available");
    expect(panel.shadowRoot?.textContent).not.toContain("Explicitly reported model input");
    expect(readableActivityContent(undefined, panel.activityContent)).toBe(panel.activityContent);
  });
});
