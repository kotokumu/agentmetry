import { msg } from "@lit/localize";
import { css, html } from "lit";
import { customElement, property } from "lit/decorators.js";
import { LocalizedElement } from "../localization/localized-element";
import { desktopUpdater, type DesktopUpdater } from "../api/desktop-updater";
import "./appearance-settings";
import "./app-update-control";
import "./language-selector";
import "./mcp-connection";
import "./retention-settings";

const claudeCommand = `{
  "env": {
    "CLAUDE_CODE_ENABLE_TELEMETRY": "1",
    "CLAUDE_CODE_ENHANCED_TELEMETRY_BETA": "1",
    "OTEL_LOGS_EXPORTER": "otlp",
    "OTEL_TRACES_EXPORTER": "otlp",
    "OTEL_METRICS_EXPORTER": "otlp",
    "OTEL_EXPORTER_OTLP_PROTOCOL": "grpc",
    "OTEL_EXPORTER_OTLP_ENDPOINT": "http://127.0.0.1:4317",
    "OTEL_LOG_USER_PROMPTS": "1",
    "OTEL_LOG_ASSISTANT_RESPONSES": "1",
    "OTEL_LOG_TOOL_DETAILS": "1"
  }
}`;

const codexCommand = `[otel]
environment = "agentmetry-local"
log_user_prompt = true
exporter = { otlp-grpc = { endpoint = "http://127.0.0.1:4317" } }
trace_exporter = { otlp-grpc = { endpoint = "http://127.0.0.1:4317" } }
metrics_exporter = { otlp-grpc = { endpoint = "http://127.0.0.1:4317" } }`;

@customElement("am-connections-settings")
export class ConnectionsSettings extends LocalizedElement {
  @property({ attribute: false }) updater: DesktopUpdater = desktopUpdater;
  private copied = "";
  private copyError = "";
  static styles = css`
    :host { display: block; }
    .stack { display: grid; gap: 16px; }
    .source-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
    article, section { border: 1px solid var(--am-border); border-radius: 10px; padding: 18px; background: var(--am-surface-raised); }
    h2, h3, p { margin: 0; }
    h2 { font-size: 1rem; }
    h3 { margin-bottom: 7px; font-size: 1rem; }
    p { color: var(--am-muted); font-size: 14px; line-height: 1.55; }
    .lead { margin-top: 7px; }
    pre { overflow: auto; margin: 14px 0 0; border: 1px solid var(--am-border); border-radius: 7px; padding: 12px; color: var(--am-text); background: var(--am-surface); font: 12px/1.5 "SFMono-Regular", "Cascadia Code", monospace; white-space: pre-wrap; }
    .copy { margin-top: 10px; border: 1px solid var(--am-border-strong); border-radius: 7px; padding: 8px 11px; color: var(--am-accent); background: var(--am-accent-soft); font: 700 12px/1.1 "SFMono-Regular", "Cascadia Code", monospace; cursor: pointer; }
    .copy:hover, .copy:focus-visible { background: rgba(var(--am-accent-rgb), .18); }
    .copy-status { min-height: 1.2em; }
    .note-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
    .controls { display: grid; gap: 18px; }
    .control { padding-top: 2px; }
    .control + .control { border-top: 1px solid var(--am-border); padding-top: 16px; }
    @media (max-width: 700px) { .source-grid, .note-grid { grid-template-columns: 1fr; } }
  `;

  render() {
    return html`<div class="stack">
      ${this.updater.supported ? html`<am-app-update-control .updater=${this.updater}></am-app-update-control>` : null}
      <section><h2>${msg("Connect sources", { id: "settingsCompletion.sourcesHeading" })}</h2><p class="lead">${msg("Copy these settings into the source configuration, then restart the source. Agentmetry does not edit source files.", { id: "settingsCompletion.sourcesIntro" })}</p></section>
      <div class="source-grid">
        <article><h3>Claude Code</h3><p>${msg("Add this env object to ~/.claude/settings.json. It enables the documented telemetry signals and content fields.", { id: "settingsCompletion.claudeIntro" })}</p><pre>${claudeCommand}</pre><button class="copy" type="button" @click=${() => this.copy("claude", claudeCommand)}>${this.copyLabel("claude")}</button></article>
        <article><h3>Codex</h3><p>${msg("Add this [otel] block to ~/.codex/config.toml. Project-local Codex configuration does not apply these settings.", { id: "settingsCompletion.codexIntro" })}</p><pre>${codexCommand}</pre><button class="copy" type="button" @click=${() => this.copy("codex", codexCommand)}>${this.copyLabel("codex")}</button></article>
      </div>
      <section><h2>${msg("Local endpoints", { id: "settingsCompletion.endpointsHeading" })}</h2><p class="lead">${msg("Defaults are OTLP gRPC 4317 and OTLP HTTP 4318. If you start Agentmetry with custom receiver ports, replace the port in the source configuration. The Web UI, HTTP API, and MCP use 17890; that browser address is not an OTLP receiver.", { id: "settingsCompletion.endpointsBody" })}</p><p class="copy-status" aria-live="polite">${this.copyError ? msg("Copy failed — select the text manually", { id: "settingsCompletion.copyFailedMessage" }) : this.copied ? msg("Copied to clipboard", { id: "settingsCompletion.copied" }) : ""}</p></section>
      <div class="note-grid">
        <section><h2>${msg("Capture scope", { id: "settingsCompletion.scopeHeading" })}</h2><p class="lead">${msg("Agentmetry retains accepted telemetry locally. What is available depends on what the source reports and which content settings are enabled.", { id: "settingsCompletion.scopeIntro" })}</p></section>
        <section><h2>${msg("Reported and retained data", { id: "settingsCompletion.retentionHeading" })}</h2><p class="lead">${msg("Prompts, responses, tool details, and file contents may be unreported, redacted, or unavailable. Missing values are shown as unreported; the app does not infer complete conversations or outcomes.", { id: "settingsCompletion.retentionBody" })}</p></section>
      </div>
      <am-retention-settings></am-retention-settings>
      <section><h2>${msg("Read-only MCP", { id: "settingsCompletion.mcpHeading" })}</h2><p class="lead">${msg("Use the local MCP URL in a client to query telemetry. MCP is stateless and read only. The Web UI origin and OTLP receiver endpoint are separate addresses.", { id: "settingsCompletion.mcpIntro" })}</p><div class="control"><am-mcp-connection inline></am-mcp-connection></div></section>
      <section class="controls">
        <div class="control"><am-language-selector></am-language-selector></div>
        <div class="control"><am-appearance-settings></am-appearance-settings></div>
      </section>
    </div>`;
  }

  private copyLabel(source: string) {
    if (this.copyError === source) return msg("Copy failed — select the text manually", { id: "settingsCompletion.copyFailed" });
    return this.copied === source ? msg("Copied", { id: "settingsCompletion.copiedButton" }) : msg("Copy configuration", { id: "settingsCompletion.copyButton" });
  }

  private async copy(source: string, value: string) {
    this.copyError = "";
    try {
      if (!navigator.clipboard) throw new Error("Clipboard API unavailable");
      await navigator.clipboard.writeText(value);
      this.copied = source;
    } catch {
      this.copied = "";
      this.copyError = source;
    }
    this.requestUpdate();
  }
}

declare global { interface HTMLElementTagNameMap { "am-connections-settings": ConnectionsSettings } }
