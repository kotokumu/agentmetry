import { conditionsKey, hasSessionConditions, parseInvestigationFilters, sessionConditions, type InvestigationFilters, type SessionConditions } from "../model/investigation-conditions";
import { css, html, type PropertyValues } from "lit";
import { customElement, state } from "lit/decorators.js";
import { LocalizedElement } from "../localization/localized-element";
import { localization } from "../localization/localization";
import "../components/app-update-control";
import "../components/conversation-workspace";
import "../components/dashboard-summary";
import "../components/language-selector";
import "../components/mcp-connection";
import "../components/connections-settings";
import "../components/time-range-filter";
import "../components/trace-explorer";
import "../components/trace-catalog";
import type { ConversationSummaryDetail, ConversationWorkspace } from "../components/conversation-workspace";
import type { DashboardStateDetail } from "../components/dashboard-summary";
import type { TraceExplorer } from "../components/trace-explorer";
import type { RangeSelectedDetail } from "../components/time-range-filter";
import { conversationTargetFromLocation, type ConversationTarget } from "../model/trace-analysis";
import type { TraceInvestigationState } from "../model/trace-investigation";
import type { TraceCatalogConditions } from "../model/trace-catalog";
import type { TelemetrySource, TimeRange } from "../model/telemetry";
import { agentmetryClient } from "../api/agentmetry-client";
import { THEME_PREFERENCE_EVENT } from "../components/theme-preference";
import { LIVE_UPDATE_EVENT, LiveUpdateController, type LiveUpdateDelivery } from "../controllers/live-update-controller";
import {
  conversationLocation,
  dashboardLocation,
  filtersFromLocation,
  canonicalSessionListLocation,
  navigationOriginFromState,
  navigationViewStateFromState,
  traceLocation,
  sectionLocation,
  type AppSection,
  type NavigationFilters,
  type NavigationOrigin,
  type NavigationViewState,
} from "./navigation";

const shortId = (value: string) => value.length > 18 ? `${value.slice(0, 14)}…` : value;

@customElement("am-app")
export class AgentmetryApp extends LocalizedElement {
  @state() private range: TimeRange = "24h";
  @state() private sourceId = "";
  @state() private search = "";
  @state() private conditions: SessionConditions = {};
  @state() private traceFailure?: TraceCatalogConditions["failureObservation"];
  @state() private traceMinDurationMs?: number;
  @state() private filterError = "";
  @state() private sessionView: "roots" | "all" = "roots";
  @state() private section: AppSection = "sessions";
  @state() private filterPending = false;
  private filterRequest = 0;
  private filterAbort?: AbortController;
  @state() private requestedConversation?: ConversationTarget;
  @state() private selectedTraceId = "";
  @state() private selectedTraceSpanId = "";
  @state() private requestedTraceInvestigation?: TraceInvestigationState;
  @state() private traceReturn?: NavigationOrigin;
  @state() private conversationReturn?: NavigationOrigin;
  @state() private workspaceInitialized = false;
  @state() private requestedAgentId = "";
  @state() private requestedPurpose: NavigationViewState["purpose"] = "execution";
  @state() private requestedActivityId = "";
  @state() private requestedFileReadId = "";
  @state() private requestedEvidenceFocus?: NavigationViewState["evidenceFocus"];
  @state() private routeAnnouncement = localization.t("app.dashboard");
  @state() private dashboardStatus: DashboardStateDetail["status"] = "loading";
  @state() private sources: readonly TelemetrySource[] = [];
  @state() private conversationStatus: ConversationSummaryDetail["status"] = "loading";
  @state() private conversationCount?: number;
  @state() private activityCount?: number;
  private pendingFocus?: "trace" | "detail" | "list";
  private pendingScrollY?: number;
  private pendingRouteView?: NavigationViewState;
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
    window.addEventListener(THEME_PREFERENCE_EVENT, this.themeChanged);
    this.syncThemeAttribute();
    this.readRoute();
    this.liveUpdates.start();
  }

  disconnectedCallback() {
    window.removeEventListener("popstate", this.popState);
    window.removeEventListener("scroll", this.scrollChanged);
    window.removeEventListener(THEME_PREFERENCE_EVENT, this.themeChanged);
    history.scrollRestoration = this.previousScrollRestoration;
    this.liveUpdates.stop();
    this.filterRequest += 1;
    this.filterAbort?.abort();
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
      background: var(--am-paper);
      font-family: Inter, ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      color-scheme: dark;
    }
    :host([data-theme="light"]) {
      --am-paper: #f5f7f9; --am-surface: #ffffff; --am-surface-raised: #f0f4f6; --am-surface-strong: #e7edf1;
      --am-text: #17232d; --am-muted: #5b6b77; --am-border: rgba(26, 55, 70, .18); --am-border-strong: rgba(0, 112, 95, .42);
      --am-accent: #087f6b; --am-accent-rgb: 8, 127, 107; --am-accent-soft: rgba(8, 127, 107, .10); --am-secondary: #3b63c6;
      --am-track: #d8e2e8; --am-danger: #b72f46; --am-success: #157a50; color-scheme: light;
    }
    :host([data-theme="dark"]) { color-scheme: dark; }
    * { box-sizing: border-box; }
    main { width: 100%; min-width: 0; max-width: 1800px; margin: 0 auto; padding: clamp(16px, 1.5vw, 24px); }
    header { position: relative; display: flex; align-items: flex-end; justify-content: space-between; gap: 24px; margin-bottom: 18px; padding: 0 2px 6px; border-bottom: 1px solid var(--am-border); }
    .brand { display: flex; width: fit-content; align-items: center; gap: 10px; margin-bottom: 12px; color: var(--am-text); font: 800 .7rem/1 "SFMono-Regular", "Cascadia Code", monospace; letter-spacing: .22em; text-decoration: none; }
    .brand:hover { color: var(--am-accent); }
    .brand:focus-visible { border-radius: 7px; outline: 2px solid var(--am-accent); outline-offset: 4px; }
    .brand-mark { width: 30px; height: 30px; padding: 2px; border: 1px solid var(--am-border-strong); border-radius: 8px; background: rgba(237, 245, 251, .92); object-fit: contain; }
    .main-nav { display: flex; flex-wrap: wrap; gap: 5px; margin: 0 0 12px; }
    .main-nav a { padding: 6px 9px; border-radius: 5px; color: var(--am-muted); font-size: 14px; text-decoration: none; }
    .main-nav a:hover, .main-nav a:focus-visible, .main-nav a[aria-current="page"] { color: var(--am-text); background: var(--am-accent-soft); }
    h1 { max-width: 720px; margin: 0; font: 650 clamp(1.4rem, 2.5vw, 2rem)/1.2 Inter, ui-sans-serif, sans-serif; letter-spacing: -.02em; }
    .header-controls { display: grid; justify-items: end; flex: 0 1 auto; min-width: min(100%, 520px); }
    .utility-controls { display: flex; align-items: flex-start; justify-content: flex-end; gap: 8px; }
    .status { display: flex; align-items: center; justify-content: flex-end; flex-wrap: wrap; gap: 5px 8px; margin: 11px 0 0; color: var(--am-muted); font: .75rem/1.45 "SFMono-Regular", "Cascadia Code", monospace; text-align: right; }
    .receiver { display: inline-flex; align-items: center; gap: 8px; }
    .state-note::before { content: "·"; margin-right: 8px; color: var(--am-muted); }
    .error { color: var(--am-danger); }
    .settings-panel { display: grid; gap: 12px; padding: 18px; border: 1px solid var(--am-border); border-radius: 8px; background: var(--am-surface-raised); }
    .settings-panel h2, .settings-panel p { margin: 0; }
    .settings-panel p { color: var(--am-muted); font-size: 14px; line-height: 1.5; }
    .sr-only { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; border: 0; }
    am-trace-explorer[hidden], am-conversation-workspace[hidden] { display: none; }
    @media (max-width: 950px) { header { align-items: flex-start; flex-direction: column; } .header-controls { justify-items: start; } .utility-controls { justify-content: flex-start; } .status { justify-content: flex-start; text-align: left; } }
    @media (max-width: 640px) { main { padding: 12px; } h1 { font-size: 1.5rem; } .brand { margin-bottom: 14px; } }
    @media (max-width: 480px) { .status { align-items: flex-start; flex-direction: column; } .state-note::before { content: none; } }
  `;

  render() {
    const traceActive = Boolean(this.selectedTraceId);
    const dashboardHref = dashboardLocation(this.filters);
    const statusText = this.statusText();
    const showDashboardSummary = this.section === "usage";
    return html`<main data-density="operator">
      <p class="sr-only" aria-live="polite">${this.routeAnnouncement}</p>
      <header>
        <div><a class="brand" href=${dashboardHref} aria-label=${localization.t("app.backToDashboard")} @click=${this.goHome}><img class="brand-mark" src="/agentmetry-mark.png" alt="" aria-hidden="true"><span>AGENTMETRY</span></a><nav class="main-nav" aria-label=${localization.t("app.mainNavigation")}>${([ ["sessions", "app.sessions"], ["traces", "app.traces"], ["usage", "app.usage"], ["connections", "app.connections"] ] as const).map(([section, label]) => html`<a href=${sectionLocation(this.filters, section)} aria-current=${this.section === section ? "page" : "false"} @click=${(event: MouseEvent) => this.sectionSelected(event, section)}>${localization.t(label)}</a>`)}</nav><h1>${localization.t(this.sectionHeadingKey())}</h1></div>
        <div class="header-controls">${this.section === "connections" ? null : html`<am-time-range-filter .selected=${this.range} @range-selected=${this.rangeSelected}></am-time-range-filter>`}${showDashboardSummary && statusText ? html`<p class="status">${statusText}</p>` : null}</div>
      </header>

      ${traceActive || !showDashboardSummary ? null : html`<am-dashboard-summary
        .range=${this.range}
        .sourceId=${this.sourceId}
        .search=${this.search}
        .conversationStatus=${this.conversationStatus}
        .conversationCount=${this.conversationCount}
        .activityCount=${this.activityCount}
        @dashboard-state-changed=${this.dashboardStateChanged}
      ></am-dashboard-summary>`}

      ${!traceActive && this.section === "traces" ? html`<am-trace-catalog .range=${this.range} .sourceId=${this.sourceId} .conditions=${this.traceConditions} .active=${true} .locationForTrace=${(traceId: string) => traceLocation(traceId, this.filters, undefined, "traces")} @trace-catalog-selected=${this.traceCatalogSelected} @trace-conditions-requested=${this.traceCatalogConditionsRequested}></am-trace-catalog>` : null}
      ${!traceActive && this.section === "connections" ? html`<am-connections-settings></am-connections-settings>` : null}

      ${traceActive ? html`<am-trace-explorer
        .traceId=${this.selectedTraceId}
        .anchorSpanId=${this.selectedTraceSpanId}
        .requestedInvestigation=${this.requestedTraceInvestigation}
        .returnHref=${this.traceReturn?.href ?? (this.section === "traces" ? sectionLocation(this.filters, "traces") : dashboardHref)}
        .returnLabel=${this.traceReturn
          ? this.localizedOriginLabel(this.traceReturn)
          : this.section === "traces" ? localization.t("app.traces") : localization.t("app.conversations")}
        .locationForConversation=${(target: ConversationTarget) => conversationLocation(target, this.filters)}
        @trace-close-requested=${this.closeTrace}
        @trace-removed=${this.traceRemoved}
        @conversation-selected-from-trace=${this.conversationSelectedFromTrace}
        @trace-view-ready=${this.traceViewReady}
        @trace-view-state-changed=${this.traceViewStateChanged}
      ></am-trace-explorer>` : null}
      ${this.workspaceInitialized && this.section === "sessions" ? html`<am-conversation-workspace
        .sessionView=${this.sessionView}
        .range=${this.range}
        .sourceId=${this.sourceId}
        .search=${this.search}
        .sources=${this.sources}
        .conditions=${this.conditions}
        .filterError=${this.filterError}
        .filterPending=${this.filterPending}
        .requestedConversation=${this.requestedConversation}
        .listHref=${dashboardHref}
        .returnHref=${this.conversationReturn?.href ?? ""}
        .returnLabel=${this.conversationReturn ? this.localizedOriginLabel(this.conversationReturn) : ""}
        .requestedAgentId=${this.requestedAgentId}
        .purpose=${this.requestedPurpose}
        .requestedActivityId=${this.requestedActivityId}
        .requestedFileReadId=${this.requestedFileReadId}
        .requestedEvidenceFocus=${this.requestedEvidenceFocus}
        .active=${!traceActive}
        ?hidden=${traceActive}
        .locationForSession=${(sourceId: string, sessionId: string) =>
          conversationLocation({ sourceId, conversationId: sessionId }, this.filters)}
        .locationForTrace=${(traceId: string, spanId?: string) => traceLocation(traceId, this.filters, spanId)}
        @investigation-filters-requested=${this.investigationFiltersRequested}
        @source-selected=${this.sourceSelected}
        @search-submitted=${this.searchSubmitted}
        @session-selected=${this.sessionSelected}
        @session-list-view-selected=${this.sessionListViewSelected}
        @trace-selected=${this.traceSelected}
		@conversation-return-requested=${this.conversationReturnRequested}
		@conversation-canonicalized=${this.conversationCanonicalized}
		@conversation-removed=${this.conversationRemoved}
        @conversation-view-ready=${this.conversationViewReady}
        @conversation-view-state-changed=${this.conversationViewStateChanged}
        @conversation-purpose-selected=${this.conversationPurposeSelected}
        @conversation-summary-changed=${this.conversationSummaryChanged}
      ></am-conversation-workspace>` : null}
    </main>`;
  }

  private rangeSelected(event: CustomEvent<RangeSelectedDetail>) {
    if (this.section === "sessions" && hasSessionConditions(this.conditions)) {
      void this.applyInvestigationFilters({ ...this.investigationFilters, range: event.detail.range });
      return;
    }
    this.range = event.detail.range;
    const href = this.selectedTraceId
      ? traceLocation(this.selectedTraceId, this.filters, this.selectedTraceSpanId || undefined, this.section === "traces" ? "traces" : undefined)
      : sectionLocation(this.filters, this.section);
    this.beginNavigation(href);
    history.pushState(history.state, "", href);
    this.readRoute(true);
  }

  private sourceSelected(event: CustomEvent<{ sourceId: string }>) {
    if (hasSessionConditions(this.conditions)) { void this.applyInvestigationFilters({ ...this.investigationFilters, sourceId: event.detail.sourceId }); return; }
    this.sourceId = event.detail.sourceId;
    this.showFilteredDashboard();
  }

  private searchSubmitted(event: CustomEvent<{ search: string }>) {
    if (hasSessionConditions(this.conditions)) { void this.applyInvestigationFilters({ ...this.investigationFilters, search: event.detail.search.trim() }); return; }
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
    this.readRoute(true);
  }

  private traceSelected(event: CustomEvent<{ traceId: string; sourceId: string; conversationId: string; spanId?: string; evidenceOrigin?: "episode" }>) {
    const originTarget: ConversationTarget = {
      sourceId: event.detail.sourceId,
      conversationId: event.detail.conversationId,
      traceId: event.detail.spanId ? event.detail.traceId : undefined,
      spanId: event.detail.spanId,
    };
    const originHref = event.detail.evidenceOrigin === "episode"
      ? conversationLocation(this.requestedConversation ?? { sourceId: event.detail.sourceId, conversationId: event.detail.conversationId }, this.filters)
      : conversationLocation(originTarget, this.filters);
    const origin: NavigationOrigin = {
      kind: "conversation",
      href: originHref,
      label: localization.t("app.conversation", { id: shortId(event.detail.conversationId) }),
    };
    this.beginNavigation(originHref);
    if (event.detail.spanId) {
      history.replaceState({ ...history.state, view: { ...history.state?.view, evidenceFocus: {
        kind: event.detail.evidenceOrigin === "episode" ? "episode" : "activity",
        traceId: event.detail.traceId, spanId: event.detail.spanId,
      } } }, "", originHref);
    }
    history.pushState({ origin }, "", traceLocation(event.detail.traceId, this.filters, event.detail.spanId));
    this.readRoute(true, true);
  }

  private conversationSelectedFromTrace(event: CustomEvent<ConversationTarget>) {
    const origin: NavigationOrigin = {
      kind: "trace",
      href: `${window.location.pathname}${window.location.search}`,
      label: localization.t("app.trace", { id: shortId(this.selectedTraceId) }),
    };
    this.beginNavigation();
    history.pushState({ origin }, "", conversationLocation(event.detail, this.filters));
    this.readRoute(true, true);
  }

  private readonly traceCatalogSelected = (event: CustomEvent<{ traceId: string }>) => {
    const origin: NavigationOrigin = {
      kind: "trace-list",
      href: `${window.location.pathname}${window.location.search}`,
      label: localization.t("app.traces"),
    };
    this.beginNavigation();
    history.pushState({ origin }, "", traceLocation(event.detail.traceId, this.filters, undefined, "traces"));
    this.readRoute(true, true);
  };

  private readonly traceCatalogConditionsRequested = (event: CustomEvent<{ conditions: TraceCatalogConditions }>) => {
    const conditions = event.detail.conditions;
    const filters = { ...this.filters, traceFailure: conditions.failureObservation, traceMinDurationMs: conditions.minDurationMs };
    this.beginNavigation();
    history.pushState({}, "", sectionLocation(filters, "traces"));
    this.readRoute(true, true);
  };

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

  private readonly sectionSelected = (event: MouseEvent, section: AppSection) => {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    const href = sectionLocation(this.filters, section);
    this.beginNavigation();
    history.pushState({}, "", href);
    this.readRoute(true, true);
  };

  private sectionHeadingKey(): "app.sessions" | "app.traces" | "app.usage" | "app.connections" {
    return `app.${this.section}` as "app.sessions" | "app.traces" | "app.usage" | "app.connections";
  }

  private readonly closeTrace = () => {
    if (this.traceReturn) {
      this.beginNavigation();
      history.back();
      return;
    }
    this.beginNavigation();
    history.replaceState({}, "", sectionLocation(this.filters, this.section));
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
    const canonical = canonicalSessionListLocation(new URL(window.location.href));
    if (canonical !== location.pathname + location.search + location.hash) history.replaceState(history.state, "", canonical);
    this.filterRequest += 1;
    this.filterAbort?.abort();
    this.filterPending = false;
    let filters = this.filters;
    try { filters = filtersFromLocation(new URL(window.location.href)); this.filterError = ""; }
    catch (error) { this.filterError = error instanceof Error ? error.message : localization.t("app.invalidUrlConditions"); }
    const conditions = sessionConditions(filters);
    if (conditionsKey(conditions) !== conditionsKey(this.conditions)) this.conditions = conditions;
    this.range = filters.range;
    this.sourceId = filters.sourceId;
    this.search = filters.search;
    this.traceFailure = filters.traceFailure;
    this.traceMinDurationMs = filters.traceMinDurationMs;
    this.sessionView = filters.sessionView ?? "roots";
    const requestedSection = new URL(window.location.href).searchParams.get("section");
    this.section = requestedSection === "traces" || requestedSection === "usage" || requestedSection === "connections" ? requestedSection : "sessions";
    const origin = navigationOriginFromState(history.state);
    const view = navigationViewStateFromState(history.state);
    const conversationTarget = conversationTargetFromLocation(window.location.pathname, window.location.search);
    const traceId = traceIdFromPath(window.location.pathname);
    this.pendingRouteView = view && (conversationTarget || traceId) ? view : undefined;
    this.requestedPurpose = view?.purpose ?? (restoreContext && view?.evidenceFocus?.kind === "episode" ? "rework" : "execution");
    this.requestedActivityId = view?.selectedActivityId ?? "";
    this.requestedFileReadId = view?.selectedFileReadId ?? "";
    this.requestedEvidenceFocus = restoreContext ? view?.evidenceFocus : undefined;
    this.pendingScrollY = restoreContext ? (resetScroll ? 0 : view?.scrollY) : undefined;
    if (resetScroll && typeof window.scrollTo === "function") window.scrollTo({ top: 0, behavior: "auto" });
    if (conversationTarget) {
      this.section = "sessions";
      this.workspaceInitialized = true;
      this.selectedTraceId = "";
      this.requestedConversation = conversationTarget;
      this.requestedAgentId = view?.selectedAgentId ?? "";
      this.traceReturn = undefined;
      this.conversationReturn = origin?.kind === "trace" ? origin : undefined;
      this.routeAnnouncement = localization.t("app.conversation", { id: shortId(conversationTarget.conversationId) });
      this.syncDocumentMetadata();
      this.pendingFocus = restoreContext ? "detail" : undefined;
      return;
    }
    this.selectedTraceId = traceId ?? "";
    this.selectedTraceSpanId = traceId ? (new URL(location.href).searchParams.get("spanId") ?? "").toLowerCase() : "";
    this.requestedTraceInvestigation = traceId && restoreContext ? view?.traceInvestigation : undefined;
    this.traceReturn = traceId && (origin?.kind === "trace-list" || origin?.kind === "conversation") ? origin : undefined;
    this.conversationReturn = undefined;
    if (traceId) {
      this.routeAnnouncement = localization.t("app.trace", { id: shortId(traceId) });
      this.syncDocumentMetadata();
      this.pendingFocus = restoreContext ? "trace" : undefined;
      return;
    }
    this.workspaceInitialized = this.section === "sessions";
    this.requestedConversation = undefined;
    this.requestedAgentId = view?.selectedAgentId ?? "";
    this.traceReturn = undefined;
    this.routeAnnouncement = localization.t("app.dashboard");
    this.syncDocumentMetadata();
    this.pendingFocus = restoreContext ? "list" : undefined;
  }

  private readonly investigationFiltersRequested = (event: CustomEvent<{ filters: InvestigationFilters }>) => {
    void this.applyInvestigationFilters(event.detail.filters);
  };

  private async applyInvestigationFilters(input: InvestigationFilters) {
    const request = ++this.filterRequest;
    this.filterAbort?.abort();
    const abort = this.filterAbort = new AbortController();
    this.filterPending = true;
    this.filterError = "";
    try {
      const filters = parseInvestigationFilters(input);
      await agentmetryClient.listSessionsPage({ ...filters, conditions: sessionConditions(filters), view: this.sessionView }, abort.signal);
      if (request !== this.filterRequest) return;
      this.beginNavigation();
      history.pushState({}, "", dashboardLocation({ ...filters, sessionView: this.sessionView }));
      this.readRoute(true, true);
    } catch (error) {
      if (request === this.filterRequest) this.filterError = error instanceof Error ? error.message : localization.t("app.conditionsCouldNotApply");
    } finally { if (request === this.filterRequest) this.filterPending = false; }
  }

  private showFilteredDashboard() {
    const href = dashboardLocation(this.filters);
    this.beginNavigation();
    if (window.location.pathname === "/") history.replaceState({}, "", href);
    else history.pushState({}, "", href);
    this.readRoute(true, true);
  }

  private get filters(): NavigationFilters {
    return {
      ...this.investigationFilters,
      ...(this.sessionView === "all" ? { sessionView: "all" as const } : {}),
      ...(this.traceFailure ? { traceFailure: this.traceFailure } : {}),
      ...(this.traceMinDurationMs !== undefined ? { traceMinDurationMs: this.traceMinDurationMs } : {}),
    };
  }

  private get traceConditions(): TraceCatalogConditions {
    return {
      ...(this.traceFailure ? { failureObservation: this.traceFailure } : {}),
      ...(this.traceMinDurationMs !== undefined ? { minDurationMs: this.traceMinDurationMs } : {}),
    };
  }

  private get investigationFilters(): InvestigationFilters {
    return { range: this.range, sourceId: this.sourceId, search: this.search, ...this.conditions };
  }

  private readonly sessionListViewSelected = (event: CustomEvent<{ view: "roots" | "all" }>) => {
    if (event.detail.view === this.sessionView) return;
    this.beginNavigation();
    const url = new URL(location.href);
    url.searchParams.delete("view");
    if (event.detail.view === "all") url.searchParams.set("view", "all");
    history.pushState(history.state, "", canonicalSessionListLocation(url));
    this.readRoute();
  };

  protected updated(_changed: PropertyValues<this>) {
    this.syncDocumentMetadata();
    this.restoreRouteContext();
  }

  private readonly conversationViewReady = () => this.restoreRouteContext();
  private readonly traceViewReady = () => this.restoreRouteContext();
  private readonly traceViewStateChanged = () => this.saveCurrentEntryView();
  private readonly conversationViewStateChanged = () => this.saveCurrentEntryView();
  private readonly conversationPurposeSelected = (event: CustomEvent<{ purpose: NavigationViewState["purpose"] }>) => {
    this.beginNavigation();
    history.pushState({ ...history.state, view: { ...history.state?.view, purpose: event.detail.purpose } }, "", location.href);
    this.readRoute(true);
  };

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
    this.saveCurrentEntryView(href, true);
  }

  private saveCurrentEntryView(href = `${window.location.pathname}${window.location.search}`, force = false) {
    const origin = navigationOriginFromState(history.state);
    const workspace = this.shadowRoot?.querySelector<ConversationWorkspace>("am-conversation-workspace");
    const workspaceVisible = !traceIdFromPath(window.location.pathname);
    const view = workspaceVisible
      ? workspace?.navigationViewState
      : this.shadowRoot?.querySelector<TraceExplorer>("am-trace-explorer")?.navigationViewState;
    if (!force && this.pendingRouteView && !routeViewApplied(this.pendingRouteView, view)) return;
    if (this.pendingRouteView && routeViewApplied(this.pendingRouteView, view)) this.pendingRouteView = undefined;
    history.replaceState({
      ...(origin ? { origin } : {}),
      view: {
        ...(view ?? {}),
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
    if (this.dashboardStatus === "loading") return localization.t("app.refreshing");
    if (this.dashboardStatus === "failed") return localization.t("app.dashboardUnavailable");
    return "";
  }

  private readonly themeChanged = () => this.syncThemeAttribute();

  private syncThemeAttribute() {
    const theme = document.documentElement.dataset.theme === "light" ? "light" : "dark";
    this.setAttribute("data-theme", theme);
  }

  private syncDocumentMetadata() {
    document.documentElement.lang = localization.locale;
    if (this.selectedTraceId) {
      this.routeAnnouncement = localization.t("app.trace", { id: shortId(this.selectedTraceId) });
      document.title = `Agentmetry · ${this.routeAnnouncement}`;
      return;
    }
    if (this.requestedConversation) {
      this.routeAnnouncement = localization.t("app.conversation", { id: shortId(this.requestedConversation.conversationId) });
      document.title = `Agentmetry · ${this.routeAnnouncement}`;
      return;
    }
    if (this.section === "connections") {
      this.routeAnnouncement = localization.t("app.connections");
      document.title = `Agentmetry · ${this.routeAnnouncement}`;
      return;
    }
    this.routeAnnouncement = localization.t("app.dashboard");
    document.title = localization.t("app.title.dashboard");
  }

  private localizedOriginLabel(origin: NavigationOrigin): string {
    const url = new URL(origin.href, window.location.origin);
    if (origin.kind === "conversation") {
      const target = conversationTargetFromLocation(url.pathname, url.search);
      return target ? localization.t("app.conversation", { id: shortId(target.conversationId) }) : localization.t("app.conversations");
    }
    const traceId = traceIdFromPath(url.pathname);
    return traceId ? localization.t("app.trace", { id: shortId(traceId) }) : origin.label;
  }
}

export function traceIdFromPath(pathname: string): string | undefined {
  const match = pathname.match(/^\/traces\/([^/]+)$/);
  if (!match) return undefined;
  try { return decodeURIComponent(match[1]); } catch { return undefined; }
}

const routeViewApplied = (requested: NavigationViewState, current?: NavigationViewState) => {
  if (!current) return false;
  if (requested.selectedAgentId !== undefined && current.selectedAgentId !== requested.selectedAgentId) return false;
  if (requested.purpose !== undefined && current.purpose !== requested.purpose) return false;
  if (requested.selectedActivityId !== undefined && current.selectedActivityId !== requested.selectedActivityId) return false;
  if (requested.selectedFileReadId !== undefined && current.selectedFileReadId !== requested.selectedFileReadId) return false;
  if (requested.traceInvestigation !== undefined && JSON.stringify(current.traceInvestigation) !== JSON.stringify(requested.traceInvestigation)) return false;
  if (requested.evidenceFocus !== undefined && JSON.stringify(current.evidenceFocus) !== JSON.stringify(requested.evidenceFocus)) return false;
  return true;
};

declare global { interface HTMLElementTagNameMap { "am-app": AgentmetryApp } }
