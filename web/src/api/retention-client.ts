import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { timestampDate, timestampFromDate } from "@bufbuild/protobuf/wkt";
import { AgentmetryRetentionService } from "../gen/agentmetry/v1/agentmetry_pb";

const client = createClient(AgentmetryRetentionService, createConnectTransport({ baseUrl: "" }));

export type RetentionPolicy = Readonly<{ enabled: boolean; archiveDays?: number; deleteDays?: number; revision: bigint; updatedAt?: Date }>;
export type RetentionOperation = Readonly<{ id: string; kind: string; status: string; phase: string; requestedAt?: Date; evaluatedAt?: Date; completedAt?: Date; affectedExports: bigint; affectedSegments: bigint; error: string }>;
export type RetentionCycle = Readonly<{ id: string; status: string; startedAt?: Date; evaluatedAt?: Date; completedAt?: Date; cohortSize: bigint; cohortUnavailableReason: string; error: string; pendingChildren: bigint; runningChildren: bigint; completedChildren: bigint; failedChildren: bigint; cancelledChildren: bigint }>;
export type ArchiveSegment = Readonly<{ id: string; minReceivedAt: Date; maxReceivedAt: Date; exportCount: bigint; originalBytes: bigint; storedBytes: bigint; payloadIntegrity: string; metadataIntegrity: string; integrityError: string; scheduledDeleteAt?: Date; scheduleUnavailableReason: string }>;
export type RetentionCapacity = Readonly<{ observedAt?: Date; totalAllocatedBytes: bigint; databaseBytes: bigint; databaseUnusedBytes: bigint; walBytes: bigint; archiveBytes: bigint; archiveAllocatedBytes: bigint; stagingAllocatedBytes: bigint; activeRawBytes: bigint; observationBytes: bigint; queryProjectionBytes: bigint; estimatedArchivePeakBytes: bigint; estimatedRestorePeakBytes: bigint; filesystemFreeBytes?: bigint; unavailableReason: string; warning: string }>;
export type RestoreResult = Readonly<{ operationId: string; accepted: boolean; result: string; currentSegmentIds: readonly string[]; activeMatches: bigint; archivedMatches: bigint; deletedMatches: bigint }>;

export const retentionClient = {
  async status(): Promise<{ policy: RetentionPolicy; operations: readonly RetentionOperation[]; cycles: readonly RetentionCycle[] }> {
    const response = await client.getRetentionStatus({});
    const policy = response.policy;
    if (!policy) throw new Error("Retention policy is unavailable");
    return {
      policy: { enabled: policy.enabled, archiveDays: policy.archiveDays, deleteDays: policy.deleteDays, revision: policy.revision, updatedAt: policy.updatedAt ? timestampDate(policy.updatedAt) : undefined },
      operations: response.operations.map((operation) => ({ id: operation.id, kind: operation.kind, status: operation.status, phase: operation.phase,
        requestedAt: operation.requestedAt ? timestampDate(operation.requestedAt) : undefined,
        evaluatedAt: operation.evaluatedAt ? timestampDate(operation.evaluatedAt) : undefined,
        completedAt: operation.completedAt ? timestampDate(operation.completedAt) : undefined,
        affectedExports: operation.affectedExports, affectedSegments: operation.affectedSegments, error: operation.error })),
      cycles: response.cycles.map((cycle) => ({ id: cycle.id, status: cycle.status, startedAt: cycle.startedAt ? timestampDate(cycle.startedAt) : undefined,
        evaluatedAt: cycle.evaluatedAt ? timestampDate(cycle.evaluatedAt) : undefined,
        completedAt: cycle.completedAt ? timestampDate(cycle.completedAt) : undefined, cohortSize: cycle.cohortSize,
        cohortUnavailableReason: cycle.cohortUnavailableReason, error: cycle.error,
        pendingChildren: cycle.pendingChildren, runningChildren: cycle.runningChildren, completedChildren: cycle.completedChildren,
        failedChildren: cycle.failedChildren, cancelledChildren: cycle.cancelledChildren })),
    };
  },
  async updatePolicy(enabled: boolean, archiveDays?: number, deleteDays?: number): Promise<void> {
    await client.updateRetentionPolicy({ enabled, archiveDays, deleteDays });
  },
  async segments(pageToken = ""): Promise<{ segments: readonly ArchiveSegment[]; nextPageToken: string }> {
    const response = await client.listArchiveSegments({ pageSize: 100, pageToken });
    return { segments: response.segments.map((segment) => ({ id: segment.id,
      minReceivedAt: timestampDate(segment.minReceivedAt!), maxReceivedAt: timestampDate(segment.maxReceivedAt!),
      exportCount: segment.exportCount, originalBytes: segment.originalBytes, storedBytes: segment.storedBytes,
      payloadIntegrity: segment.payloadIntegrity, metadataIntegrity: segment.metadataIntegrity,
      integrityError: segment.integrityError,
      scheduledDeleteAt: segment.scheduledDeleteAt ? timestampDate(segment.scheduledDeleteAt) : undefined,
      scheduleUnavailableReason: segment.scheduleUnavailableReason })), nextPageToken: response.nextPageToken };
  },
  async capacity(): Promise<RetentionCapacity> {
    const response = await client.getRetentionCapacity({});
    return { observedAt: response.observedAt ? timestampDate(response.observedAt) : undefined, totalAllocatedBytes: response.totalAllocatedBytes,
      databaseBytes: response.databaseBytes, databaseUnusedBytes: response.databaseUnusedBytes,
      walBytes: response.walBytes, archiveBytes: response.archiveBytes, archiveAllocatedBytes: response.archiveAllocatedBytes,
      stagingAllocatedBytes: response.stagingAllocatedBytes, activeRawBytes: response.activeRawBytes,
      observationBytes: response.observationBytes, queryProjectionBytes: response.queryProjectionBytes,
      estimatedArchivePeakBytes: response.estimatedArchivePeakBytes, estimatedRestorePeakBytes: response.estimatedRestorePeakBytes,
      filesystemFreeBytes: response.filesystemFreeBytes, unavailableReason: response.unavailableReason, warning: response.warning };
  },
  async restoreSegment(segmentId: string, holdDays: number): Promise<RestoreResult> {
    const response = await client.restoreArchive({ scope: { case: "segmentId", value: segmentId }, holdDays });
    return { operationId: response.operationId, accepted: response.accepted, result: response.result, currentSegmentIds: response.currentSegmentIds,
      activeMatches: response.activeMatches, archivedMatches: response.archivedMatches, deletedMatches: response.deletedMatches };
  },
  async restorePeriod(start: Date, end: Date, holdDays: number): Promise<RestoreResult> {
    const response = await client.restoreArchive({ scope: { case: "receiveTime", value: { start: timestampFromDate(start), end: timestampFromDate(end) } }, holdDays });
    return { operationId: response.operationId, accepted: response.accepted, result: response.result, currentSegmentIds: response.currentSegmentIds,
      activeMatches: response.activeMatches, archivedMatches: response.archivedMatches, deletedMatches: response.deletedMatches };
  },
};
