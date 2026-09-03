import { conditionsKey, type InvestigationFilters } from "../model/investigation-conditions";
export type TelemetryFilters = InvestigationFilters;
export const telemetryFilterKey = (value: TelemetryFilters) => `${value.range}\u0000${value.sourceId}\u0000${value.search}\u0000${conditionsKey(value)}`;
