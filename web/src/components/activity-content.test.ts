import { afterEach, describe, expect, it } from "vitest";
import { html, LitElement } from "lit";
import type { Activity } from "../model/telemetry";
import { activityContentPreview, activityContentStyles, reportedReferences, renderActivityContent } from "./activity-content";
import { localization } from "../localization/localization";
import type { ContentEvidencePanel } from "./content-evidence";

const activity = (overrides: Partial<Activity> = {}): Activity => ({
  id: "activity-a", source: "claude", signal: "log", name: "gen_ai.tool_result", kind: "tool",
  agentId: "main", runId: "conversation-a", model: "", observedAt: "2026-09-08T00:00:00Z",
  tokens: { input: null, output: null, cacheRead: null, cacheWrite: null, reasoning: null, total: null },
  contributesToTotal: false,
  ...overrides,
});

afterEach(async () => {
  document.body.replaceChildren();
  await localization.select("en");
});

class ActivityContentHost extends LitElement {
  activity?: Activity;
  static styles = activityContentStyles;
  render() { return this.activity ? renderActivityContent(this.activity) : html``; }
}
if (!customElements.get("test-activity-content-host")) customElements.define("test-activity-content-host", ActivityContentHost);

describe("activity content presentation", () => {
  it("presents an explicit document reference with its name and exact path", async () => {
    const value = String.raw`C:\workspace\project\docs\runbook.md`;
    const referenceActivity = activity({
      content: value,
      contentEvidence: { source: "claude", activityId: "activity-a", signal: "log", kind: "reference", evidence: "reference", availability: "not_reported", fields: ["file_path"], truncated: false },
    });
    const view = new ActivityContentHost();
    view.activity = referenceActivity;
    document.body.append(view);
    await view.updateComplete;

    expect(reportedReferences(referenceActivity)).toEqual([{ name: "runbook.md", reference: value, sourceField: "file_path", role: "file" }]);
    expect(view.shadowRoot?.querySelector(".document-name")?.textContent).toBe("runbook.md");
    expect(view.shadowRoot?.querySelector(".document-reference")?.textContent).toBe(value);
    expect(view.shadowRoot?.querySelector("h4")?.textContent).toBe("Reported file");
    expect(view.shadowRoot?.textContent).toContain("Only the reference was reported");
    expect(view.shadowRoot?.querySelector("pre.received-content")).toBeNull();
    expect(activityContentPreview(referenceActivity)).toBe("runbook.md");
  });

  it("lists multiple explicitly structured references and retains the raw tool input", async () => {
    const raw = JSON.stringify({ file_paths: ["AGENTS.md", "docs/operations/runbook.md"], note: "keep exactly" });
    const toolInput = activity({
      content: raw,
      contentEvidence: { source: "claude", activityId: "activity-a", signal: "log", kind: "tool_input", evidence: "unknown", availability: "available", fields: ["tool_input"], truncated: false },
    });
    const view = new ActivityContentHost();
    view.activity = toolInput;
    document.body.append(view);
    await view.updateComplete;

    expect(reportedReferences(toolInput)).toEqual([
      { name: "AGENTS.md", reference: "AGENTS.md", sourceField: "tool_input", argumentKey: "file_paths", role: "file" },
      { name: "runbook.md", reference: "docs/operations/runbook.md", sourceField: "tool_input", argumentKey: "file_paths", role: "file" },
    ]);
    expect(view.shadowRoot?.querySelectorAll(".document-item")).toHaveLength(2);
    expect(view.shadowRoot?.querySelector("pre.received-content")?.textContent).toBe(raw);
    expect(view.shadowRoot?.textContent).toContain("Telemetry field: tool_input");
    expect(view.shadowRoot?.textContent).toContain("Tool input key: file_paths");
    expect(activityContentPreview(toolInput)).toBe("AGENTS.md · runbook.md — note: keep exactly");
  });

  it("does not infer document references from prose or shell command text", () => {
    const prompt = activity({
      content: "Review AGENTS.md and docs/runbook.md",
      contentEvidence: { source: "claude", activityId: "activity-a", signal: "log", kind: "prompt", evidence: "unknown", availability: "available", fields: ["prompt"], truncated: false },
    });
    const command = activity({
      content: "cat AGENTS.md",
      contentEvidence: { source: "claude", activityId: "activity-b", signal: "log", kind: "tool_input", evidence: "unknown", availability: "available", fields: ["full_command"], truncated: false },
    });

    expect(reportedReferences(prompt)).toEqual([]);
    expect(reportedReferences(command)).toEqual([]);
  });

  it("does not reveal a redacted reference or derive a complete identity from truncated evidence", async () => {
    const redacted = activity({
      content: "/private/docs/secret.md",
      contentEvidence: { source: "codex", activityId: "activity-a", signal: "log", kind: "reference", evidence: "reference", availability: "redacted", fields: ["file_path"], truncated: false, redactionReason: "producer_redacted" },
    });
    const truncated = activity({
      content: "/workspace/docs/runbook.md",
      contentEvidence: { source: "claude", activityId: "activity-b", signal: "log", kind: "reference", evidence: "reference", availability: "available", fields: ["file_path"], truncated: true },
    });

    expect(reportedReferences(redacted)).toEqual([]);
    expect(reportedReferences(truncated)).toEqual([{ name: "/workspace/docs/runbook.md", reference: "/workspace/docs/runbook.md", sourceField: "file_path", role: "file" }]);

    const redactedView = new ActivityContentHost();
    redactedView.activity = redacted;
    document.body.append(redactedView);
    await redactedView.updateComplete;
    const redactedEvidence = redactedView.shadowRoot?.querySelector<ContentEvidencePanel>("am-content-evidence");
    await redactedEvidence?.updateComplete;
    expect(redactedEvidence?.shadowRoot?.textContent).toContain("Producer-redacted");
    expect(redactedView.shadowRoot?.textContent).not.toContain("/private/docs/secret.md");
    expect(redactedView.shadowRoot?.querySelector(".document-item")).toBeNull();

    const truncatedView = new ActivityContentHost();
    truncatedView.activity = truncated;
    document.body.append(truncatedView);
    await truncatedView.updateComplete;
    expect(truncatedView.shadowRoot?.querySelector(".document-name")?.textContent).toBe("/workspace/docs/runbook.md");
    const truncatedEvidence = truncatedView.shadowRoot?.querySelector<ContentEvidencePanel>("am-content-evidence");
    await truncatedEvidence?.updateComplete;
    expect(truncatedEvidence?.shadowRoot?.textContent).toContain("Received content is truncated.");
  });

  it("renders localized reference semantics and a structured file list", async () => {
    await localization.select("ja");
    const raw = JSON.stringify({ file_paths: ["AGENTS.md", "docs/guide.md"] });
    const view = new ActivityContentHost();
    view.activity = activity({
      content: raw,
      contentEvidence: { source: "claude", activityId: "activity-a", signal: "log", kind: "tool_input", evidence: "unknown", availability: "available", fields: ["tool_input"], truncated: false },
    });
    document.body.append(view);
    await view.updateComplete;

    const root = view.shadowRoot!;
    expect(root.querySelector("h4")?.textContent).toBe("報告されたファイル");
    expect(root.querySelectorAll("ul.document-list")).toHaveLength(1);
    expect(root.querySelectorAll("ul.document-list > li")).toHaveLength(2);
    expect(root.querySelector(".document-detail-label")?.textContent).toBe("ファイルパス");
    expect(root.querySelectorAll(".document-field")[0]?.textContent).toBe("Telemetryフィールド: tool_input");
    expect(root.querySelectorAll(".document-field")[1]?.textContent).toBe("ツール入力キー: file_paths");
    expect(root.querySelector("pre.received-content")?.textContent).toBe(raw);
  });

  it("distinguishes a request-body pointer from a reported file", async () => {
    const bodyReference = activity({
      content: "file:///private/request.json",
      contentEvidence: { source: "claude", activityId: "activity-a", signal: "log", kind: "reference", evidence: "reference", availability: "not_reported", fields: ["body_ref"], truncated: false },
    });
    const view = new ActivityContentHost();
    view.activity = bodyReference;
    document.body.append(view);
    await view.updateComplete;

    expect(reportedReferences(bodyReference)).toEqual([{
      name: "request.json", reference: "file:///private/request.json", sourceField: "body_ref", role: "request_body",
    }]);
    expect(view.shadowRoot?.querySelector("h4")?.textContent).toBe("Request body reference");
    expect(view.shadowRoot?.textContent).not.toContain("Referenced document");
  });

  it("does not promote nested payload metadata into a reported file", () => {
    const raw = JSON.stringify({ metadata: { file_path: "AGENTS.md" } });
    const toolInput = activity({
      content: raw,
      contentEvidence: { source: "claude", activityId: "activity-a", signal: "log", kind: "tool_input", evidence: "unknown", availability: "available", fields: ["tool_input"], truncated: false },
    });

    expect(reportedReferences(toolInput)).toEqual([]);
    expect(activityContentPreview(toolInput)).toBe(raw);
  });
});
