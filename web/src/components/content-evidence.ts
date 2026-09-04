import { css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import type { ContentEvidence } from "../model/telemetry";
import { LocalizedElement } from "../localization/localized-element";
import { localization } from "../localization/localization";
import type { MessageKey } from "../localization/messages";

const kinds: Record<ContentEvidence["kind"], MessageKey> = {
  prompt: "content.kind.prompt", response: "content.kind.response", tool_input: "content.kind.toolInput",
  tool_output: "content.kind.toolOutput", tool_input_output: "content.kind.toolInputOutput",
  model_input: "content.kind.modelInput", reference: "content.kind.reference", unknown: "common.unknown",
};
const strengths: Record<ContentEvidence["evidence"], MessageKey> = {
  reference: "content.kind.reference", read_output: "content.strength.readOutput",
  explicit_model_input: "content.strength.explicitModelInput", unknown: "common.unknown",
};
const availability: Record<ContentEvidence["availability"], MessageKey> = {
  available: "content.availability.available", not_reported: "content.availability.notReported", redacted: "content.availability.redacted", not_returned: "content.availability.notReturned",
};

export const readableActivityContent = (evidence: ContentEvidence | undefined, content: string | undefined): string =>
  evidence?.availability === "redacted" || evidence?.availability === "not_returned" ? "" : content ?? "";
export const contentAvailabilityLabel = (evidence: ContentEvidence | undefined, content: string | undefined): string =>
  localization.t(availability[evidence?.availability ?? (content ? "available" : "not_reported")]);

@customElement("am-content-evidence")
export class ContentEvidencePanel extends LocalizedElement {
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
      <dt>${localization.t("content.kind")}</dt><dd>${localization.t(kinds[evidence?.kind ?? "unknown"])}</dd>
      <dt>${localization.t("content.evidence")}</dt><dd>${localization.t(strengths[evidence?.evidence ?? "unknown"])}</dd>
      <dt>${localization.t("content.availability")}</dt><dd>${contentAvailabilityLabel(evidence, this.activityContent)}</dd>
      ${evidence?.fields.length ? html`<dt>${localization.t("content.receivedFields")}</dt><dd>${evidence.fields.join(", ")}</dd>` : null}
    </dl>
    ${evidence?.evidence !== "explicit_model_input" ? html`<p>${localization.t("content.unconfirmed")}</p>` : null}
    ${evidence?.truncated ? html`<p>${localization.t("content.truncated")}</p>` : null}
    ${evidence?.redactionReason === "encrypted_input" ? html`<p>${localization.t("content.encrypted")}</p>` : null}`;
  }
}

declare global { interface HTMLElementTagNameMap { "am-content-evidence": ContentEvidencePanel } }
