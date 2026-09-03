import { LitElement, css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { ContentEvidence } from "../model/telemetry";

const kinds: Record<ContentEvidence["kind"], string> = {
  prompt: "Received prompt", response: "Received response", tool_input: "Received tool input",
  tool_output: "Received tool output", tool_input_output: "Received tool input and output",
  model_input: "Reported model input", reference: "Reference only", unknown: "Unknown",
};
const strengths: Record<ContentEvidence["evidence"], string> = {
  reference: "Reference only", read_output: "Received read or tool output",
  explicit_model_input: "Explicitly reported model input", unknown: "Unknown",
};
const availability: Record<ContentEvidence["availability"], string> = {
  available: "Body available", not_reported: "Body not reported", redacted: "Producer-redacted", not_returned: "Body not requested",
};

export const readableActivityContent = (evidence: ContentEvidence | undefined, content: string | undefined): string =>
  evidence?.availability === "redacted" || evidence?.availability === "not_returned" ? "" : content ?? "";
export const contentAvailabilityLabel = (evidence: ContentEvidence | undefined, content: string | undefined): string =>
  availability[evidence?.availability ?? (content ? "available" : "not_reported")];

@customElement("am-content-evidence")
export class ContentEvidencePanel extends LitElement {
  @property({ attribute: false }) evidence?: ContentEvidence;
  @property() activityContent = "";

  static styles = css`
    :host { display: block; min-width: 0; color: var(--am-muted); font-size: .75rem; line-height: 1.5; overflow-wrap: anywhere; }
    dl { display: grid; grid-template-columns: minmax(0, 7rem) minmax(0, 1fr); gap: 5px 12px; margin: 12px 0; }
    dd { margin: 0; color: var(--am-text); min-width: 0; }
    p { margin: 8px 0; }
  `;

  render() {
    const evidence = this.evidence;
    return html`<dl>
      <dt>Content kind</dt><dd>${kinds[evidence?.kind ?? "unknown"]}</dd>
      <dt>Evidence</dt><dd>${strengths[evidence?.evidence ?? "unknown"]}</dd>
      <dt>Availability</dt><dd>${contentAvailabilityLabel(evidence, this.activityContent)}</dd>
      ${evidence?.fields.length ? html`<dt>Received fields</dt><dd>${evidence.fields.join(", ")}</dd>` : null}
    </dl>
    ${evidence?.evidence !== "explicit_model_input" ? html`<p>Model-input inclusion is unconfirmed.</p>` : null}
    ${evidence?.truncated ? html`<p>Received content is truncated.</p>` : null}
    ${evidence?.redactionReason === "encrypted_input" ? html`<p>Input was encrypted. Only readable received content is shown.</p>` : null}`;
  }
}

declare global { interface HTMLElementTagNameMap { "am-content-evidence": ContentEvidencePanel } }
