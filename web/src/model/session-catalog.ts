import type { SessionConditions } from "./investigation-conditions";
import type { Session, TimeRange } from "./telemetry";

export type SessionListView = "roots" | "all";
export type SessionName = Readonly<{
  text: string;
  origin: "claude_code.generate_session_title";
  observedAt?: string;
}>;
export type SessionCatalog = Readonly<{
  role: "root" | "child";
  rootSessionId: string;
  parentSessionId: string;
  name?: SessionName;
}>;
export type SessionListEntry = Session & Readonly<{ catalog?: SessionCatalog }>;
export type SessionListPage = Readonly<{ sessions: readonly SessionListEntry[]; nextPageToken: string }>;
export type SessionListQuery = Readonly<{
  range: TimeRange; sourceId: string; search: string;
  conditions: SessionConditions; view: SessionListView; pageSize?: number;
}>;
export interface SessionListReader {
  listSessionsPage(query: SessionListQuery & Readonly<{ pageToken?: string }>, signal?: AbortSignal): Promise<SessionListPage>;
}
