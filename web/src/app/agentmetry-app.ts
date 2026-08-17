import { LitElement, css, html, type PropertyValues } from "lit";
import { customElement, state } from "lit/decorators.js";
import "../components/app-update-control";
import "../components/conversation-workspace";
import "../components/dashboard-summary";
import "../components/time-range-filter";
import "../components/trace-explorer";
import type { ConversationSummaryDetail, ConversationWorkspace } from "../components/conversation-workspace";
import type { DashboardStateDetail } from "../components/dashboard-summary";
import type { TraceExplorer } from "../components/trace-explorer";
import type { RangeSelectedDetail } from "../components/time-range-filter";
import { conversationTargetFromLocation, type ConversationTarget } from "../model/trace-analysis";
import type { TelemetrySource, TimeRange } from "../model/telemetry";
import { agentmetryClient } from "../api/agentmetry-client";
import { LIVE_UPDATE_EVENT, LiveUpdateController, type LiveUpdateDelivery } from "../controllers/live-update-controller";
import {
  conversationLocation,
  dashboardLocation,
  filtersFromLocation,
  navigationOriginFromState,
  navigationViewStateFromState,
  traceLocation,
  type NavigationFilters,
  type NavigationOrigin,
} from "./navigation";

@customElement("am-app")
export class AgentmetryApp extends LitElement {
  @state() private range: TimeRange = "24h";
  @state() private sourceId = "";
  @state() private search = "";
  @state() private requestedConversation?: ConversationTarget;
  @state() private selectedTraceId = "";
  @state() private traceReturn?: NavigationOrigin;
  @state() private conversationReturn?: NavigationOrigin;
  @state() private workspaceInitialized = false;
  @state() private requestedAgentId = "";
  @state() private routeAnnouncement = "Dashboard";
  @state() private dashboardStatus: DashboardStateDetail["status"] = "loading";
  @state() private sources: readonly TelemetrySource[] = [];
  @state() private conversationStatus: ConversationSummaryDetail["status"] = "loading";
  @state() private conversationCount?: number;
  @state() private activityCount?: number;
  private pendingFocus?: "trace" | "detail" | "list";
  private pendingScrollY?: number;
  private scrollSaveGeneration = 0;
  private scrollSaveScheduled = false;
  private previousScrollRestoration: ScrollRestoration = "auto";
  private readonly liveUpdates = new LiveUpdateController(agentmetryClient, async (windowValue) => {
    const pending: Promise<unknown>[] = [];
    const detail: LiveUpdateDelivery = { ...windowValue, waitUntil: (promise) => pending.push(promise) };
    window.dispatchEvent(new CustomEvent(LIVE_UPDATE_EVENT, { detail }));
    await Promise.all(pending);
  });

  connectedCallback() {
    super.connectedCallback();
    this.previousScrollRestoration = history.scrollRestoration;
    history.scrollRestoration = "manual";
    window.addEventListener("popstate", this.popState);
    window.addEventListener("scroll", this.scrollChanged, { passive: true });
    this.readRoute();
    this.liveUpdates.start();
  }

  disconnectedCallback() {
    window.removeEventListener("popstate", this.popState);
    window.removeEventListener("scroll", this.scrollChanged);
    history.scrollRestoration = this.previousScrollRestoration;
    this.liveUpdates.stop();
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
    .brand-mark { width: 30px; height: 30px; padding: 2px; border: 1px solid var(--am-border-strong); border-radius: 8px; background: rgba(237, 245, 251, .92); object-fit: contain; box-shadow: 0 0 20px rgba(var(--am-accent-rgb), .12); }
    .eyebrow { margin: 0 0 6px; color: var(--am-accent); font: 700 .64rem/1 "SFMono-Regular", "Cascadia Code", monospace; letter-spacing: .15em; text-transform: uppercase; }
    h1 { max-width: 720px; margin: 0; font: 650 clamp(2.2rem, 4vw, 4.2rem)/.9 Inter, ui-sans-serif, sans-serif; letter-spacing: -.065em; }
    h1 span { color: var(--am-muted); font-weight: 400; }
    .header-controls { display: grid; justify-items: end; flex: 0 1 auto; min-width: min(100%, 520px); }
    .status { display: flex; align-items: center; justify-content: flex-end; flex-wrap: wrap; gap: 5px 8px; margin: 11px 0 0; color: var(--am-muted); font: .7rem/1.45 "SFMono-Regular", "Cascadia Code", monospace; text-align: right; }
    .receiver { display: inline-flex; align-items: center; gap: 8px; }
    .state-note::before { content: "·"; margin-right: 8px; color: var(--am-muted); }
    .status-dot { width: 7px; height: 7px; flex: 0 0 auto; border-radius: 50%; background: var(--am-success); box-shadow: 0 0 0 4px rgba(101, 230, 165, .09), 0 0 14px rgba(101, 230, 165, .55); animation: pulse 2.8s ease-in-out infinite; }
    .error { color: var(--am-danger); }
    .sr-only { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; border: 0; }
    am-trace-explorer[hidden], am-conversation-workspace[hidden] { display: none; }
    @keyframes pulse { 50% { box-shadow: 0 0 0 6px rgba(101, 230, 165, .03), 0 0 20px rgba(101, 230, 165, .7); } }
    @media (max-width: 950px) { header { align-items: flex-start; flex-direction: column; } .header-controls { justify-items: start; } .status { justify-content: flex-start; text-align: left; } }
    @media (max-width: 640px) { main { padding: 12px; } h1 { font-size: clamp(2.35rem, 12vw, 4rem); } .brand { margin-bottom: 14px; } }
    @media (max-width: 480px) { .status { align-items: flex-start; flex-direction: column; } .state-note::before { content: none; } }
    @media (prefers-reduced-motion: reduce) { .status-dot { animation: none; } }
  `;

  render() {
    const traceActive = Boolean(this.selectedTraceId);
    const dashboardHref = dashboardLocation(this.filters);
    return html`<main data-density="operator">
      <p class="sr-only" aria-live="polite">${this.routeAnnouncement}</p>
      ${traceActive ? null : html`<header>
        <div><a class="brand" href=${dashboardHref} aria-label="Back to Agentmetry dashboard" @click=${this.goHome}><img class="brand-mark" src="/agentmetry-mark.png" alt="" aria-hidden="true"><span>AGENTMETRY</span></a><p class="eyebrow">Local trace observatory // Live</p><h1>Agent conversations,<br><span>decoded.</span></h1></div>
        <div class="header-controls"><am-app-update-control></am-app-update-control><am-time-range-filter .selected=${this.range} @range-selected=${this.rangeSelected}></am-time-range-filter><p class="status">${this.statusText()}</p></div>
      </header>`}

      ${traceActive ? null : html`<am-dashboard-summary
        .range=${this.range}
        .sourceId=${this.sourceId}
        .search=${this.search}
        .conversationStatus=${this.conversationStatus}
        .conversationCount=${this.conversationCount}
        .activityCount=${this.activityCount}
        @dashboard-state-changed=${this.dashboardStateChanged}
      ></am-dashboard-summary>`}

      ${traceActive ? html`<am-trace-explorer
        .traceId=${this.selectedTraceId}
        .returnHref=${this.traceReturn?.href ?? dashboardHref}
        .returnLabel=${this.traceReturn?.label ?? "Conversations"}
        .locationForConversation=${(target: ConversationTarget) => conversationLocation(target, this.filters)}
        @trace-close-requested=${this.closeTrace}
        @trace-removed=${this.traceRemoved}
        @conversation-selected-from-trace=${this.conversationSelectedFromTrace}
        @trace-view-ready=${this.traceViewReady}
      ></am-trace-explorer>` : null}
      ${this.workspaceInitialized ? html`<am-conversation-workspace
        .range=${this.range}
        .sourceId=${this.sourceId}
        .search=${this.search}
        .sources=${this.sources}
        .requestedConversation=${this.requestedConversation}
        .listHref=${dashboardHref}
        .returnHref=${this.conversationReturn?.href ?? ""}
        .returnLabel=${this.conversationReturn?.label ?? ""}
        .requestedAgentId=${this.requestedAgentId}
        .active=${!traceActive}
        ?hidden=${traceActive}
        .locationForSession=${(sourceId: string, sessionId: string) =>
          conversationLocation({ sourceId, conversationId: sessionId }, this.filters)}
        .locationForTrace=${(traceId: string) => traceLocation(traceId, this.filters)}
        @source-selected=${this.sourceSelected}
        @search-submitted=${this.searchSubmitted}
        @session-selected=${this.sessionSelected}
        @trace-selected=${this.traceSelected}
		@conversation-return-requested=${this.conversationReturnRequested}
		@conversation-canonicalized=${this.conversationCanonicalized}
		@conversation-removed=${this.conversationRemoved}
        @conversation-view-ready=${this.conversationViewReady}
        @conversation-view-state-changed=${this.conversationViewStateChanged}
        @conversation-summary-changed=${this.conversationSummaryChanged}
      ></am-conversation-workspace>` : null}
    </main>`;
  }

  private rangeSelected(event: CustomEvent<RangeSelectedDetail>) {
    this.range = event.detail.range;
    this.showFilteredDashboard();
  }

  private sourceSelected(event: CustomEvent<{ sourceId: string }>) {
    this.sourceId = event.detail.sourceId;
    this.showFilteredDashboard();
  }

  private searchSubmitted(event: CustomEvent<{ search: string }>) {
    this.search = event.detail.search.trim();
    if (this.requestedConversation) {
      const href = conversationLocation(this.requestedConversation, this.filters);
      this.beginNavigation();
      history.replaceState({}, "", href);
      this.readRoute(true, true);
      return;
    }
    this.showFilteredDashboard();
  }

  private sessionSelected(event: CustomEvent<{ sessionId: string; sourceId: string }>) {
    const target = { sourceId: event.detail.sourceId, conversationId: event.detail.sessionId };
    this.beginNavigation();
    history.pushState({}, "", conversationLocation(target, this.filters));
    this.readRoute(true, true);
  }

  private traceSelected(event: CustomEvent<{ traceId: string; sourceId: string; conversationId: string; spanId?: string }>) {
    const originTarget: ConversationTarget = {
      sourceId: event.detail.sourceId,
      conversationId: event.detail.conversationId,
      traceId: event.detail.spanId ? event.detail.traceId : undefined,
      spanId: event.detail.spanId,
    };
    const originHref = conversationLocation(originTarget, this.filters);
    const origin: NavigationOrigin = {
      kind: "conversation",
      href: originHref,
      label: `Conversation ${shortId(event.detail.conversationId)}`,
    };
    this.beginNavigation(originHref);
    history.pushState({ origin }, "", traceLocation(event.detail.traceId, this.filters));
    this.readRoute(true, true);
  }

  private conversationSelectedFromTrace(event: CustomEvent<ConversationTarget>) {
    const origin: NavigationOrigin = {
      kind: "trace",
      href: `${window.location.pathname}${window.location.search}`,
      label: `Trace ${shortId(this.selectedTraceId)}`,
    };
    this.beginNavigation();
    history.pushState({ origin }, "", conversationLocation(event.detail, this.filters));
    this.readRoute(true, true);
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
    const href = dashboardLocation(this.filters);
    this.beginNavigation();
    if (`${window.location.pathname}${window.location.search}` !== href) history.pushState({}, "", href);
    this.readRoute(true, true);
  };

  private readonly closeTrace = () => {
    if (this.traceReturn) {
      this.beginNavigation();
      history.back();
      return;
    }
    this.beginNavigation();
    history.replaceState({}, "", dashboardLocation(this.filters));
    this.readRoute(true, true);
  };

  private readonly conversationReturnRequested = (event: CustomEvent<{ to: "origin" | "list" }>) => {
    if (event.detail.to === "origin" && this.conversationReturn) {
      this.beginNavigation();
      history.back();
      return;
    }
    this.showFilteredDashboard();
  };

	private readonly conversationRemoved = (event: CustomEvent<{ sourceId: string; conversationId: string }>) => {
    if (this.requestedConversation?.sourceId !== event.detail.sourceId || this.requestedConversation.conversationId !== event.detail.conversationId) return;
    if (this.selectedTraceId) {
      // The trace remains a valid standalone view even when its navigation
      // origin disappears. Drop only the stale return target and keep the
      // currently visible trace mounted.
      this.requestedConversation = undefined;
      this.traceReturn = undefined;
      const state = history.state && typeof history.state === "object" ? history.state : {};
      history.replaceState({ ...state, origin: undefined }, "", `${location.pathname}${location.search}`);
      return;
    }
    this.beginNavigation();
    history.replaceState({}, "", dashboardLocation(this.filters));
    this.readRoute(true, true);
	};

	private readonly conversationCanonicalized = (event: CustomEvent<ConversationTarget>) => {
	  const requested = this.requestedConversation;
	  if (!requested || requested.sourceId !== event.detail.sourceId || requested.conversationId === event.detail.conversationId) return;
	  const state = history.state && typeof history.state === "object" ? history.state : {};
	  history.replaceState(state, "", conversationLocation(event.detail, this.filters));
	  this.readRoute();
	};

  private readonly traceRemoved = (event: CustomEvent<{ traceId: string }>) => {
    if (this.selectedTraceId !== event.detail.traceId) return;
    this.beginNavigation();
    history.replaceState({}, "", dashboardLocation(this.filters));
    this.readRoute(true, true);
  };

  private readonly popState = () => this.readRoute(true);

  private readRoute(restoreContext = false, resetScroll = false) {
    const filters = filtersFromLocation(new URL(window.location.href));
    this.range = filters.range;
    this.sourceId = filters.sourceId;
    this.search = filters.search;
    const origin = navigationOriginFromState(history.state);
    const view = navigationViewStateFromState(history.state);
    this.pendingScrollY = restoreContext ? (resetScroll ? 0 : view?.scrollY) : undefined;
    if (resetScroll && typeof window.scrollTo === "function") window.scrollTo({ top: 0, behavior: "auto" });
    const conversationTarget = conversationTargetFromLocation(window.location.pathname, window.location.search);
    if (conversationTarget) {
      this.workspaceInitialized = true;
      this.selectedTraceId = "";
      this.requestedConversation = conversationTarget;
      this.requestedAgentId = view?.selectedAgentId ?? "";
      this.traceReturn = undefined;
      this.conversationReturn = origin?.kind === "trace" ? origin : undefined;
      this.routeAnnouncement = `Conversation ${shortId(conversationTarget.conversationId)}`;
      document.title = `Agentmetry · ${this.routeAnnouncement}`;
      this.pendingFocus = restoreContext ? "detail" : undefined;
      return;
    }
    const traceId = traceIdFromPath(window.location.pathname);
    this.selectedTraceId = traceId ?? "";
    this.traceReturn = traceId && origin?.kind === "conversation" ? origin : undefined;
    this.conversationReturn = undefined;
    if (traceId) {
      this.routeAnnouncement = `Trace ${shortId(traceId)}`;
      document.title = `Agentmetry · ${this.routeAnnouncement}`;
      this.pendingFocus = restoreContext ? "trace" : undefined;
      return;
    }
    this.workspaceInitialized = true;
    this.requestedConversation = undefined;
    this.requestedAgentId = view?.selectedAgentId ?? "";
    this.traceReturn = undefined;
    this.routeAnnouncement = "Dashboard";
    document.title = "Agentmetry · Local AI Agent Observability";
    this.pendingFocus = restoreContext ? "list" : undefined;
  }

  private showFilteredDashboard() {
    const href = dashboardLocation(this.filters);
    this.beginNavigation();
    if (window.location.pathname === "/") history.replaceState({}, "", href);
    else history.pushState({}, "", href);
    this.readRoute(true, true);
  }

  private get filters(): NavigationFilters {
    return { range: this.range, sourceId: this.sourceId, search: this.search };
  }

  protected updated(_changed: PropertyValues<this>) {
    this.restoreRouteContext();
  }

  private readonly conversationViewReady = () => this.restoreRouteContext();
  private readonly traceViewReady = () => this.restoreRouteContext();
  private readonly conversationViewStateChanged = () => this.saveCurrentEntryView();

  private readonly scrollChanged = () => {
    if (this.scrollSaveScheduled) return;
    this.scrollSaveScheduled = true;
    const generation = this.scrollSaveGeneration;
    requestAnimationFrame(() => {
      this.scrollSaveScheduled = false;
      if (generation === this.scrollSaveGeneration) this.saveCurrentEntryView();
    });
  };

  private beginNavigation(href?: string) {
    this.scrollSaveGeneration += 1;
    this.scrollSaveScheduled = false;
    this.saveCurrentEntryView(href);
  }

  private saveCurrentEntryView(href = `${window.location.pathname}${window.location.search}`) {
    const origin = navigationOriginFromState(history.state);
    const workspace = this.shadowRoot?.querySelector<ConversationWorkspace>("am-conversation-workspace");
    const workspaceVisible = !traceIdFromPath(window.location.pathname);
    history.replaceState({
      ...(origin ? { origin } : {}),
      view: {
        ...(workspaceVisible ? workspace?.navigationViewState : {}),
        scrollY: window.scrollY,
      },
    }, "", href);
  }

  private restoreRouteContext() {
    let focusReady = this.pendingFocus === undefined;
    if (this.pendingFocus === "trace") {
      const trace = this.shadowRoot?.querySelector<TraceExplorer>("am-trace-explorer");
      trace?.focusRouteHeading();
      focusReady = Boolean(trace);
    } else if (this.pendingFocus === "detail" || this.pendingFocus === "list") {
      const workspace = this.shadowRoot?.querySelector<ConversationWorkspace>("am-conversation-workspace");
      focusReady = workspace?.focusRouteHeading(this.pendingFocus) ?? false;
    }
    if (!focusReady) return;
    this.pendingFocus = undefined;
    const scrollY = this.pendingScrollY;
    if (scrollY === undefined || typeof window.scrollTo !== "function") return;
    if (this.selectedTraceId) {
      const trace = this.shadowRoot?.querySelector<TraceExplorer>("am-trace-explorer");
      if (!trace?.viewReady) return;
    }
    this.pendingScrollY = undefined;
    requestAnimationFrame(() => requestAnimationFrame(() => window.scrollTo({ top: scrollY, behavior: "auto" })));
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

const shortId = (value: string) => value.length > 18 ? `${value.slice(0, 14)}…` : value;

declare global { interface HTMLElementTagNameMap { "am-app": AgentmetryApp } }
