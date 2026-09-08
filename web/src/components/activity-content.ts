import { css, html } from "lit";
import type { Activity } from "../model/telemetry";
import { localization } from "../localization/localization";
import { contentKindLabel, readableActivityContent } from "./content-evidence";
import "./content-evidence";

export type ReportedReference = Readonly<{
  name: string;
  reference: string;
  sourceField: string;
  argumentKey?: string;
  role: "file" | "request_body" | "reference";
}>;

const structuredContentFields = new Set(["tool_input", "tool_parameters"]);
const fileArgumentKeys = new Set(["file_path", "file_paths"]);

const referenceName = (reference: string, derive: boolean): string => {
  if (!derive) return reference;
  const withoutSuffix = reference.split(/[?#]/, 1)[0].replace(/[\\/]+$/, "");
  const segments = withoutSuffix.split(/[\\/]/).filter(Boolean);
  return segments.at(-1) ?? reference;
};

const referenceValues = (value: unknown): readonly string[] => {
  if (typeof value === "string") return value ? [value] : [];
  if (!Array.isArray(value)) return [];
  return value.filter((item): item is string => typeof item === "string" && item.length > 0);
};

const structuredFileReferences = (
  content: string,
  sourceField: string,
  deriveName: boolean,
): readonly ReportedReference[] => {
  let root: unknown;
  try { root = JSON.parse(content); } catch { return []; }
  if (!root || Array.isArray(root) || typeof root !== "object") return [];
  const result: ReportedReference[] = [];
  const seen = new Set<string>();
  for (const [argumentKey, value] of Object.entries(root)) {
    if (!fileArgumentKeys.has(argumentKey)) continue;
    for (const reference of referenceValues(value)) {
      if (seen.has(reference)) continue;
      seen.add(reference);
      result.push({ name: referenceName(reference, deriveName), reference, sourceField, argumentKey, role: "file" });
    }
  }
  return result;
};

const structuredInputPreview = (content: string): string => {
  let root: unknown;
  try { root = JSON.parse(content); } catch { return content; }
  if (!root || Array.isArray(root) || typeof root !== "object") return content;
  return Object.entries(root)
    .filter(([key]) => !fileArgumentKeys.has(key))
    .map(([key, value]) => {
      const rendered = typeof value === "string" ? value : JSON.stringify(value);
      return rendered === undefined ? "" : `${key}: ${rendered}`;
    })
    .filter(Boolean)
    .join(" · ");
};

export const reportedReferences = (activity: Activity): readonly ReportedReference[] => {
  const evidence = activity.contentEvidence;
  const content = readableActivityContent(evidence, activity.content);
  if (!evidence || !content) return [];
  const deriveName = !evidence.truncated;
  if (evidence.kind === "reference") {
    const sourceField = evidence.fields[0] ?? "reference";
    const role = sourceField === "body_ref" ? "request_body" : sourceField === "file_path" ? "file" : "reference";
    return [{ name: referenceName(content, deriveName), reference: content, sourceField, role }];
  }
  const sourceField = evidence.fields.find((field) => structuredContentFields.has(field));
  if ((evidence.kind === "tool_input" || evidence.kind === "tool_input_output") && sourceField) {
    return structuredFileReferences(content, sourceField, deriveName);
  }
  return [];
};

export const activityContentPreview = (activity: Activity): string => {
  const references = reportedReferences(activity);
  const content = readableActivityContent(activity.contentEvidence, activity.content);
  if (references.length === 0) return content;
  const names = references.map(({ name }) => name).join(" · ");
  if (activity.contentEvidence?.kind === "reference") return names;
  const inputPreview = structuredInputPreview(content);
  return inputPreview ? `${names} — ${inputPreview}` : names;
};

export const activityContentStyles = css`
    h4 { margin: 16px 0 8px; font-size: .875rem; }
    .document-list { display: grid; gap: 8px; margin: 0; padding: 0; list-style: none; }
    .document-item { display: grid; gap: 5px; padding: 10px 12px; border: 1px solid var(--am-border); border-left: 3px solid var(--am-accent); border-radius: 6px; background: var(--am-surface-strong); }
    .document-name { color: var(--am-text); font-size: .875rem; overflow-wrap: anywhere; }
    .document-detail { display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 4px 8px; align-items: baseline; }
    .document-detail-label { color: var(--am-muted); font-size: .75rem; }
    .document-reference { color: var(--am-muted); font: .75rem/1.5 "SFMono-Regular", "Cascadia Code", monospace; overflow-wrap: anywhere; }
    .document-field { color: var(--am-muted); font-size: .75rem; }
    .document-availability { margin: 9px 0 0; color: var(--am-muted); font-size: .75rem; line-height: 1.5; }
    pre.received-content { margin: 8px 0 0; padding: 11px 12px; border: 1px solid var(--am-border); border-radius: 6px; background: var(--am-surface-strong); color: var(--am-text); white-space: pre-wrap; overflow-wrap: anywhere; font: .875rem/1.65 "SFMono-Regular", "Cascadia Code", monospace; }
    .empty-content { margin: 0; color: var(--am-muted); font-size: .8rem; line-height: 1.6; }
`;

const referencesHeading = (references: readonly ReportedReference[]): string => {
  if (references.every(({ role }) => role === "file")) {
    return localization.t(references.length === 1 ? "activity.reportedFile" : "activity.reportedFiles");
  }
  if (references.every(({ role }) => role === "request_body")) return localization.t("activity.requestBodyReference");
  return localization.t(references.length === 1 ? "activity.reportedReference" : "activity.reportedReferences");
};

export const renderActivityContent = (activity: Activity) => {
  const evidence = activity.contentEvidence;
  const content = readableActivityContent(evidence, activity.content);
  const references = reportedReferences(activity);
  const directReference = evidence?.kind === "reference";
  return html`
    ${references.length > 0 ? html`
      <h4>${referencesHeading(references)}</h4>
      <ul class="document-list">${references.map(({ name, reference, sourceField, argumentKey, role }) => html`<li class="document-item">
        <strong class="document-name">${name}</strong>
        <span class="document-detail"><span class="document-detail-label">${localization.t(role === "file" ? "activity.filePath" : "activity.reference")}</span><code class="document-reference">${reference}</code></span>
        <small class="document-field">${localization.t("activity.reportedVia", { field: sourceField })}</small>
        ${argumentKey ? html`<small class="document-field">${localization.t("activity.toolInputKey", { key: argumentKey })}</small>` : null}
      </li>`)}</ul>
      ${directReference ? html`<p class="document-availability">${localization.t("activity.referenceContentNotReported")}</p>` : html`
        <h4>${contentKindLabel(evidence)}</h4>
        <pre class="received-content">${content}</pre>
      `}
    ` : html`
      <h4>${evidence ? contentKindLabel(evidence) : localization.t("activity.receivedBody")}</h4>
      ${content ? html`<pre class="received-content">${content}</pre>` : html`<p class="empty-content">${localization.t(!evidence || evidence.availability === "not_reported" ? "activity.noBodyReported" : "activity.noReadableBody")}</p>`}
    `}
    <am-content-evidence .evidence=${evidence} .activityContent=${activity.content ?? ""}></am-content-evidence>
  `;
};
