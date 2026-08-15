import type { TimeRange } from "../model/telemetry";

export type TelemetryFilters = Readonly<{
  range: TimeRange;
  sourceId: string;
  search: string;
}>;

export const telemetryFilterKey = ({ range, sourceId, search }: TelemetryFilters) => `${range}\u0000${sourceId}\u0000${search}`;
