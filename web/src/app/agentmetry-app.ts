import { LitElement, css, html } from "lit";
import { customElement, state } from "lit/decorators.js";
import "../components/conversation-workspace";
import "../components/dashboard-summary";
import "../components/time-range-filter";
import "../components/trace-explorer";
import type { ConversationSummaryDetail } from "../components/conversation-workspace";
import type { DashboardStateDetail } from "../components/dashboard-summary";
import type { RangeSelectedDetail } from "../components/time-range-filter";
import { conversationTargetFromLocation, type ConversationTarget } from "../model/trace-analysis";
import type { TelemetrySource, TimeRange } from "../model/telemetry";

@customElement("am-app")
export class AgentmetryApp extends LitElement {
  @state() private range: TimeRange = "24h";
  @state() private sourceId = "";
  @state() private search = "";
  @state() private requestedConversation?: ConversationTarget;
  @state() private selectedTraceId = "";
  @state() private dashboardStatus: DashboardStateDetail["status"] = "loading";
  @state() private sources: readonly TelemetrySource[] = [];
  @state() private conversationStatus: ConversationSummaryDetail["status"] = "loading";
  @state() private conversationCount?: number;
  @state() private activityCount?: number;

  connectedCallback() {
    super.connectedCallback();
    window.addEventListener("popstate", this.popState);
    this.readRoute();
  }

  disconnectedCallback() {
    window.removeEventListener("popstate", this.popState);
    super.disconnectedCallback();
  }

  static styles = css`
    :host {
      --am-paper: #070a0f;
      --am-surface: #0d121a;
      --am-surface-raised: #121923;
      --am-surface-strong: #151e29;
      --am-text: #edf5fb;
      --am-muted: #8795a6;
      --am-border: rgba(155, 190, 213, .16);
      --am-border-strong: rgba(109, 244, 214, .38);
      --am-accent: #6df4d6;
      --am-accent-rgb: 109, 244, 214;
      --am-accent-soft: rgba(109, 244, 214, .11);
      --am-secondary: #8ba6ff;
      --am-track: #1a2430;
      --am-danger: #ff7082;
      --am-success: #65e6a5;
      display: block;
      min-height: 100vh;
      color: var(--am-text);
      background:
        radial-gradient(circle at 12% -8%, rgba(109, 244, 214, .12), transparent 28rem),
        radial-gradient(circle at 92% 2%, rgba(139, 166, 255, .10), transparent 32rem),
        linear-gradient(90deg, rgba(154, 190, 214, .035) 1px, transparent 1px) 0 0 / 28px 28px,
        linear-gradient(rgba(154, 190, 214, .035) 1px, transparent 1px) 0 0 / 28px 28px,
        var(--am-paper);
      font-family: Inter, ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      color-scheme: dark;
    }
    * { box-sizing: border-box; }
    main { width: 100%; min-width: 0; max-width: 1800px; margin: 0 auto; padding: clamp(16px, 1.5vw, 24px); }
    header { position: relative; display: flex; align-items: flex-end; justify-content: space-between; gap: 24px; margin-bottom: 18px; padding: 0 2px 6px; }
    header::after { content: ""; position: absolute; inset: auto 0 -8px; height: 1px; background: linear-gradient(90deg, var(--am-accent), rgba(109, 244, 214, .08) 42%, transparent); }
    .brand { display: flex; width: fit-content; align-items: center; gap: 10px; margin-bottom: 12px; color: var(--am-text); font: 800 .7rem/1 "SFMono-Regular", "Cascadia Code", monospace; letter-spacing: .22em; text-decoration: none; }
    .brand:hover { color: var(--am-accent); }
    .brand:focus-visible { border-radius: 7px; outline: 2px solid var(--am-accent); outline-offset: 4px; }
    .brand-mark { position: relative; width: 24px; height: 24px; border: 1px solid var(--am-border-strong); border-radius: 7px; background: var(--am-accent-soft); box-shadow: inset 0 0 16px rgba(var(--am-accent-rgb), .08), 0 0 20px rgba(var(--am-accent-rgb), .1); }
    .brand-mark::before, .brand-mark::after { content: ""; position: absolute; background: var(--am-accent); }
    .brand-mark::before { width: 9px; height: 2px; top: 7px; left: 7px; box-shadow: 0 7px 0 var(--am-accent); }
    .brand-mark::after { width: 2px; height: 9px; top: 7px; left: 7px; box-shadow: 7px 0 0 var(--am-accent); opacity: .45; }
    .eyebrow { margin: 0 0 6px; color: var(--am-accent); font: 700 .64rem/1 "SFMono-Regular", "Cascadia Code", monospace; letter-spacing: .15em; text-transform: uppercase; }
    h1 { max-width: 720px; margin: 0; font: 650 clamp(2.2rem, 4vw, 4.2rem)/.9 Inter, ui-sans-serif, sans-serif; letter-spacing: -.065em; }
    h1 span { color: var(--am-muted); font-weight: 400; }
    .header-controls { display: grid; justify-items: end; flex: 0 1 auto; min-width: min(100%, 520px); }
    .status { display: flex; align-items: center; justify-content: flex-end; flex-wrap: wrap; gap: 5px 8px; margin: 11px 0 0; color: var(--am-muted); font: .7rem/1.45 "SFMono-Regular", "Cascadia Code", monospace; text-align: right; }
    .receiver { display: inline-flex; align-items: center; gap: 8px; }
    .state-note::before { content: "·"; margin-right: 8px; color: var(--am-muted); }
    .status-dot { width: 7px; height: 7px; flex: 0 0 auto; border-radius: 50%; background: var(--am-success); box-shadow: 0 0 0 4px rgba(101, 230, 165, .09), 0 0 14px rgba(101, 230, 165, .55); animation: pulse 2.8s ease-in-out infinite; }
    .error { color: var(--am-danger); }
    am-trace-explorer[hidden], am-conversation-workspace[hidden] { display: none; }
    @keyframes pulse { 50% { box-shadow: 0 0 0 6px rgba(101, 230, 165, .03), 0 0 20px rgba(101, 230, 165, .7); } }
    @media (max-width: 950px) { header { align-items: flex-start; flex-direction: column; } .header-controls { justify-items: start; } .status { justify-content: flex-start; text-align: left; } }
    @media (max-width: 640px) { main { padding: 12px; } h1 { font-size: clamp(2.35rem, 12vw, 4rem); } .brand { margin-bottom: 14px; } }
    @media (max-width: 480px) { .status { align-items: flex-start; flex-direction: column; } .state-note::before { content: none; } }
    @media (prefers-reduced-motion: reduce) { .status-dot { animation: none; } }
  `;

  render() {
    return html`<main data-density="operator">
      <header>
        <div><a class="brand" href="/" aria-label="Back to Agentmetry dashboard" @click=${this.goHome}><span class="brand-mark" aria-hidden="true"></span><span>AGENTMETRY</span></a><p class="eyebrow">Local trace observatory // Live</p><h1>Agent conversations,<br><span>decoded.</span></h1></div>
        <div class="header-controls"><am-time-range-filter .selected=${this.range} @range-selected=${this.rangeSelected}></am-time-range-filter><p class="status">${this.statusText()}</p></div>
      </header>

      <am-dashboard-summary
        .range=${this.range}
        .sourceId=${this.sourceId}
        .search=${this.search}
        .conversationStatus=${this.conversationStatus}
        .conversationCount=${this.conversationCount}
        .activityCount=${this.activityCount}
        @dashboard-state-changed=${this.dashboardStateChanged}
      ></am-dashboard-summary>

      <am-trace-explorer
        .traceId=${this.selectedTraceId}
        ?hidden=${!this.selectedTraceId}
        @trace-close-requested=${this.closeTrace}
      ></am-trace-explorer>
      <am-conversation-workspace
        .range=${this.range}
        .sourceId=${this.sourceId}
        .search=${this.search}
        .sources=${this.sources}
        .requestedConversation=${this.requestedConversation}
        .active=${!this.selectedTraceId}
        ?hidden=${Boolean(this.selectedTraceId)}
        @source-selected=${this.sourceSelected}
        @search-submitted=${this.searchSubmitted}
        @session-selected=${this.sessionSelected}
        @conversation-summary-changed=${this.conversationSummaryChanged}
      ></am-conversation-workspace>
    </main>`;
  }

  private rangeSelected(event: CustomEvent<RangeSelectedDetail>) {
    this.range = event.detail.range;
    this.requestedConversation = undefined;
  }

  private sourceSelected(event: CustomEvent<{ sourceId: string }>) {
    this.sourceId = event.detail.sourceId;
    this.requestedConversation = undefined;
  }

  private searchSubmitted(event: CustomEvent<{ search: string }>) {
    this.search = event.detail.search.trim();
    this.requestedConversation = undefined;
  }

  private sessionSelected(event: CustomEvent<{ sessionId: string; sourceId: string }>) {
    const target = { sourceId: event.detail.sourceId, conversationId: event.detail.sessionId };
    history.pushState({}, "", conversationPath(target.sourceId, target.conversationId));
    this.requestedConversation = target;
  }

  private dashboardStateChanged(event: CustomEvent<DashboardStateDetail>) {
    this.dashboardStatus = event.detail.status;
    this.sources = event.detail.sources;
  }

  private conversationSummaryChanged(event: CustomEvent<ConversationSummaryDetail>) {
    this.conversationStatus = event.detail.status;
    this.conversationCount = event.detail.conversationCount;
    this.activityCount = event.detail.activityCount;
  }

  private readonly goHome = (event: MouseEvent) => {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    if (window.location.pathname !== "/" || window.location.search) history.pushState({}, "", "/");
    this.selectedTraceId = "";
    this.requestedConversation = undefined;
  };

  private readonly closeTrace = () => {
    if (shouldReturnThroughHistory(document.referrer, window.location.origin, history.length)) {
      history.back();
      return;
    }
    history.replaceState({}, "", "/");
    this.selectedTraceId = "";
    this.requestedConversation = undefined;
  };

  private readonly popState = () => this.readRoute();

  private readRoute() {
    const conversationTarget = conversationTargetFromLocation(window.location.pathname, window.location.search);
    if (conversationTarget) {
      this.selectedTraceId = "";
      this.requestedConversation = conversationTarget;
      return;
    }
    const traceId = traceIdFromPath(window.location.pathname);
    this.selectedTraceId = traceId ?? "";
    this.requestedConversation = undefined;
  }

  private statusText() {
    const receiver = html`<span class="receiver"><span class="status-dot" aria-label="Receiving OTLP"></span><span>Receiving OTLP locally · HTTP :4318 · gRPC :4317</span></span>`;
    if (this.dashboardStatus === "loading") return html`${receiver}<span class="state-note">Refreshing dashboard…</span>`;
    if (this.dashboardStatus === "failed") return html`${receiver}<span class="state-note error">Dashboard data unavailable</span>`;
    return receiver;
  }
}

export function traceIdFromPath(pathname: string): string | undefined {
  const match = pathname.match(/^\/traces\/([^/]+)$/);
  if (!match) return undefined;
  try { return decodeURIComponent(match[1]); } catch { return undefined; }
}

export function shouldReturnThroughHistory(referrer: string, origin: string, historyLength: number): boolean {
  if (!referrer || historyLength <= 1) return false;
  try {
    const url = new URL(referrer);
    return url.origin === origin && (url.pathname === "/" || conversationTargetFromLocation(url.pathname, url.search) !== undefined);
  } catch {
    return false;
  }
}

export const conversationPath = (sourceId: string, conversationId: string) =>
  `/conversations/${encodeURIComponent(sourceId)}/${encodeURIComponent(conversationId)}`;

declare global { interface HTMLElementTagNameMap { "am-app": AgentmetryApp } }
