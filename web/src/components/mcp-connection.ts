import { css, html, type PropertyValues } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import { LocalizedElement } from "../localization/localized-element";
import { localization } from "../localization/localization";

type CopyStatus = "idle" | "copied" | "failed";

export const mcpEndpointFromOrigin = (origin: string) => new URL("/mcp", origin).href;

@customElement("am-mcp-connection")
export class MCPConnection extends LocalizedElement {
  @property() endpoint = mcpEndpointFromOrigin(window.location.origin);
  @state() private open = false;
  @state() private copyStatus: CopyStatus = "idle";
  private copyGeneration = 0;

  static styles = css`
    :host { position: relative; display: block; min-height: 31px; margin-bottom: 10px; }
    * { box-sizing: border-box; }
    button { font: inherit; }
    .disclosure {
      display: inline-flex;
      min-height: 31px;
      align-items: center;
      gap: 7px;
      border: 1px solid var(--am-border);
      border-radius: 8px;
      padding: 7px 10px;
      color: var(--am-text);
      background: rgba(18, 25, 35, .82);
      font: 700 .64rem/1 "SFMono-Regular", "Cascadia Code", monospace;
      letter-spacing: .04em;
      cursor: pointer;
    }
    .disclosure:hover,
    .disclosure:focus-visible,
    .disclosure[aria-expanded="true"] {
      border-color: var(--am-border-strong);
      color: var(--am-accent);
      background: var(--am-accent-soft);
    }
    button:focus-visible { outline: 2px solid var(--am-accent); outline-offset: 2px; }
    .signal { color: var(--am-accent); font: 700 .58rem/1 "SFMono-Regular", "Cascadia Code", monospace; letter-spacing: -.08em; }
    .action { color: var(--am-muted); font-weight: 500; }
    .panel {
      position: absolute;
      z-index: 20;
      top: calc(100% + 2px);
      right: 0;
      width: min(460px, calc(100vw - 24px));
      border: 1px solid var(--am-border-strong);
      border-radius: 12px;
      padding: 17px;
      background: linear-gradient(145deg, rgba(18, 25, 35, .99), rgba(7, 10, 15, .99));
      box-shadow: 0 24px 70px rgba(0, 0, 0, .48), inset 0 1px 0 rgba(255, 255, 255, .035);
      text-align: left;
    }
    .panel[hidden] { display: none; }
    .eyebrow { margin: 0 0 6px; color: var(--am-accent); font: 700 .61rem/1 "SFMono-Regular", "Cascadia Code", monospace; letter-spacing: .15em; text-transform: uppercase; }
    h2 { margin: 0; color: var(--am-text); font: 650 1rem/1.25 Inter, ui-sans-serif, sans-serif; }
    .intro { margin: 7px 0 14px; color: var(--am-muted); font: .75rem/1.55 Inter, ui-sans-serif, sans-serif; }
    .label { display: block; margin-bottom: 6px; color: var(--am-muted); font: 700 .6rem/1 "SFMono-Regular", "Cascadia Code", monospace; letter-spacing: .11em; text-transform: uppercase; }
    .endpoint { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 8px; align-items: stretch; }
    .endpoint-value { width: 100%; min-width: 0; border: 1px solid var(--am-border); border-radius: 8px; padding: 10px; color: var(--am-text); background: rgba(3, 7, 11, .72); font: .7rem/1.35 "SFMono-Regular", "Cascadia Code", monospace; }
    .endpoint-value:focus { border-color: var(--am-border-strong); outline: 2px solid var(--am-accent); outline-offset: 2px; }
    .copy { border: 1px solid var(--am-border-strong); border-radius: 8px; padding: 0 12px; color: var(--am-accent); background: var(--am-accent-soft); font: 700 .63rem/1 "SFMono-Regular", "Cascadia Code", monospace; cursor: pointer; }
    .copy:hover { background: rgba(var(--am-accent-rgb), .18); }
    .copy-status { min-height: 1em; margin: 6px 0 0; color: var(--am-muted); font: .62rem/1.3 "SFMono-Regular", "Cascadia Code", monospace; }
    .copy-status.failed { color: var(--am-danger); }
    dl { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; margin: 13px 0; }
    dl div { min-width: 0; border-top: 1px solid var(--am-border); padding-top: 8px; }
    dt { margin-bottom: 4px; color: var(--am-muted); font: 700 .57rem/1 "SFMono-Regular", "Cascadia Code", monospace; letter-spacing: .08em; text-transform: uppercase; }
    dd { margin: 0; color: var(--am-text); font: 650 .67rem/1.35 Inter, ui-sans-serif, sans-serif; }
    .workflow { margin: 0; border-left: 2px solid var(--am-accent); padding: 2px 0 2px 10px; color: var(--am-muted); font: .7rem/1.5 Inter, ui-sans-serif, sans-serif; }
    .workflow code { color: var(--am-accent); font: .67rem/1.5 "SFMono-Regular", "Cascadia Code", monospace; }
    @media (max-width: 950px) { .panel { right: auto; left: 0; } }
    @media (max-width: 480px) { dl { grid-template-columns: 1fr; } .endpoint { grid-template-columns: 1fr; } .copy { min-height: 34px; } }
  `;

  render() {
    return html`
      <button
        class="disclosure"
        type="button"
        aria-expanded=${this.open ? "true" : "false"}
        aria-controls="mcp-connection-panel"
        @click=${this.togglePanel}
        @keydown=${this.keyDown}
      ><span class="signal" aria-hidden="true">//</span><span>MCP</span><span class="action">${localization.t(this.open ? "mcp.close" : "mcp.details")}</span></button>
      <section id="mcp-connection-panel" class="panel" ?hidden=${!this.open} aria-labelledby="mcp-connection-title" @keydown=${this.keyDown}>
        <p class="eyebrow">${localization.t("mcp.eyebrow")}</p>
        <h2 id="mcp-connection-title">${localization.t("mcp.title")}</h2>
        <p class="intro">${localization.t("mcp.intro")}</p>
        <span class="label">${localization.t("mcp.serverUrl")}</span>
        <div class="endpoint">
          <input class="endpoint-value" aria-label=${localization.t("mcp.serverUrlAria")} .value=${this.endpoint} readonly @focus=${this.selectEndpoint}>
          <button class="copy" type="button" @click=${this.copyEndpoint}>${localization.t(this.copyStatus === "copied" ? "mcp.copied" : "mcp.copyUrl")}</button>
        </div>
        <p class="copy-status ${this.copyStatus === "failed" ? "failed" : ""}" aria-live="polite">${this.copyMessage}</p>
        <dl>
          <div><dt>${localization.t("mcp.transport")}</dt><dd>Streamable HTTP</dd></div>
          <div><dt>${localization.t("mcp.authentication")}</dt><dd>${localization.t("mcp.noAuthentication")}</dd></div>
          <div><dt>${localization.t("mcp.access")}</dt><dd>${localization.t("mcp.readOnly")}</dd></div>
        </dl>
        <p class="workflow">${localization.t("mcp.workflowBefore")} <code>get_agent_context</code> ${localization.t("mcp.workflowMiddle")} <code>source</code> ${localization.t("mcp.workflowAnd")} <code>runId</code>.</p>
      </section>
    `;
  }

  protected updated(changed: PropertyValues<this>) {
    if (!changed.has("endpoint")) return;
    this.copyGeneration += 1;
    if (this.copyStatus !== "idle") this.copyStatus = "idle";
  }

  private readonly togglePanel = () => {
    if (this.open) this.dismissPanel();
    else this.open = true;
  };

  private readonly copyEndpoint = async () => {
    const endpoint = this.endpoint;
    const generation = ++this.copyGeneration;
    try {
      if (!navigator.clipboard) throw new Error("Clipboard API unavailable");
      await navigator.clipboard.writeText(endpoint);
      if (generation !== this.copyGeneration || !this.open || endpoint !== this.endpoint) return;
      this.copyStatus = "copied";
    } catch {
      if (generation !== this.copyGeneration || !this.open || endpoint !== this.endpoint) return;
      this.copyStatus = "failed";
    }
  };

  private readonly keyDown = (event: KeyboardEvent) => {
    if (event.key !== "Escape" || !this.open) return;
    event.preventDefault();
    event.stopPropagation();
    this.dismissPanel(true);
  };

  private dismissPanel(restoreFocus = false) {
    this.copyGeneration += 1;
    this.open = false;
    this.copyStatus = "idle";
    if (restoreFocus) void this.updateComplete.then(() => this.shadowRoot?.querySelector<HTMLButtonElement>(".disclosure")?.focus());
  }

  private readonly selectEndpoint = (event: FocusEvent) => {
    (event.currentTarget as HTMLInputElement).select();
  };

  private get copyMessage() {
    if (this.copyStatus === "copied") return localization.t("mcp.copiedMessage");
    if (this.copyStatus === "failed") return localization.t("mcp.copyFailed");
    return "";
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "am-mcp-connection": MCPConnection;
  }
}
