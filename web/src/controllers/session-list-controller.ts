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
    if (!this.isActive()) { if (this.key) this.deactivate(); return; }
    if (this.connected && this.key !== queryKey(this.query())) void this.refresh();
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
    if (!this.isActive()) { this.deactivate(); return Promise.resolve(); }
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

  private deactivate() {
    this.generation += 1;
    this.abort?.abort();
    this.key = "";
    this.rows = [];
    this.nextToken = "";
    this.pageRequest = undefined;
    this.loading = false;
    this.loadingMore = false;
    this.failed = false;
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
    const current = () => this.connected && this.isActive() && generation === this.generation && key === queryKey(this.query());
    try {
      const page = await this.reader.listSessionsPage(query, abort.signal);
      if (!current()) return;
      this.rows = uniqueRows(mode === "append" ? [...this.rows, ...page.sessions] : page.sessions);
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
