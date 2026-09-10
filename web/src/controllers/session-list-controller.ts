import type { ReactiveController, ReactiveControllerHost } from "lit";
import { conditionsKey, sessionConditions } from "../model/investigation-conditions";
import type { SessionListEntry, SessionListQuery, SessionListReader } from "../model/session-catalog";

const queryKey = (value: SessionListQuery) => JSON.stringify([value.range, value.sourceId, value.search, conditionsKey(value.conditions), value.view, value.pageSize ?? 100]);
const uniqueRows = (rows: readonly SessionListEntry[]) => [...new Map(rows.map((row) => [JSON.stringify([row.sourceId, row.id]), row])).values()];

// Owns list queries and their resident pages. Detail responses never enter here.
export class SessionListController implements ReactiveController {
  private connected = false;
  private key = "";
  private rows: readonly SessionListEntry[] = [];
  private nextToken = "";
  private generation = 0;
  private abort?: AbortController;
  private pageRequest?: Promise<void>;
  private refreshPending = false;
  loading = false;
  loadingMore = false;
  failed = false;

  constructor(private readonly host: ReactiveControllerHost, private readonly reader: SessionListReader, private readonly query: () => SessionListQuery, private readonly isActive: () => boolean = () => true) {
    host.addController(this);
  }

  get sessions() { return this.isActive() && this.key === queryKey(this.query()) ? this.rows : []; }
  get hasMore() { return this.isActive() && this.key === queryKey(this.query()) && Boolean(this.nextToken); }
  get error() { return this.failed ? "Session list unavailable" : undefined; }

  hostConnected() { this.connected = true; void this.refresh(); }
  hostUpdate() {
    const nextKey = queryKey(this.query());
    if (!this.isActive()) {
      if (this.key && this.key !== nextKey) this.invalidate();
      return;
    }
    if (!this.connected) return;
    if (this.key !== nextKey) void this.refresh();
    else if (this.refreshPending) {
      this.refreshPending = false;
      void this.refresh();
    }
  }
  hostDisconnected() {
    this.connected = false;
    this.generation += 1;
    this.abort?.abort();
    this.pageRequest = undefined;
    this.loading = false;
    this.loadingMore = false;
  }

  refresh(): Promise<void> {
    if (!this.connected) return Promise.resolve();
    if (!this.isActive()) {
      if (this.key) this.refreshPending = true;
      return Promise.resolve();
    }
    this.refreshPending = false;
    const key = queryKey(this.query());
    if (this.key !== key) { this.rows = []; this.nextToken = ""; }
    this.key = key;
    this.pageRequest = undefined;
    return this.read("replace");
  }

  loadMore(): Promise<void> {
    if (!this.connected || this.loading || !this.hasMore) return Promise.resolve();
    if (this.pageRequest) return this.pageRequest;
    this.pageRequest = this.read("append");
    return this.pageRequest;
  }

  private invalidate() {
    this.generation += 1;
    this.abort?.abort();
    this.abort = undefined;
    this.key = "";
    this.rows = [];
    this.nextToken = "";
    this.pageRequest = undefined;
    this.loading = false;
    this.loadingMore = false;
    this.failed = false;
    this.refreshPending = false;
  }

  private async read(mode: "replace" | "append") {
    const value = this.query();
    const query = { ...value, conditions: sessionConditions(value.conditions), pageToken: mode === "append" ? this.nextToken : "" };
    const key = this.key;
    const generation = ++this.generation;
    this.abort?.abort();
    const abort = new AbortController();
    this.abort = abort;
    this.loading = mode === "replace";
    this.loadingMore = mode === "append";
    this.failed = false;
    this.host.requestUpdate();
    const current = () => this.connected && generation === this.generation && key === queryKey(this.query());
    try {
      let page = await this.reader.listSessionsPage(query, abort.signal);
      const residentCount = this.rows.length;
      const refreshed = [...page.sessions];
      const tokens = new Set<string>();
      // Live refresh replaces the whole resident range, not just its first page.
      while (mode === "replace" && current() && refreshed.length < residentCount && page.nextPageToken) {
        if (tokens.has(page.nextPageToken)) throw new Error("Repeated session page token");
        tokens.add(page.nextPageToken);
        page = await this.reader.listSessionsPage({ ...query, pageToken: page.nextPageToken }, abort.signal);
        refreshed.push(...page.sessions);
      }
      if (!current()) return;
      this.rows = uniqueRows(mode === "append" ? [...this.rows, ...page.sessions] : refreshed);
      this.nextToken = page.nextPageToken;
    } catch {
      if (!current()) return;
      this.failed = true;
    } finally {
      if (current()) {
        this.loading = false;
        this.loadingMore = false;
        this.pageRequest = undefined;
        this.host.requestUpdate();
      }
    }
  }
}
