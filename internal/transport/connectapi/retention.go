package connectapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/kotokumu/agentmetry/gen/agentmetry/v1"
	"github.com/kotokumu/agentmetry/gen/agentmetry/v1/agentmetryv1connect"
	"github.com/kotokumu/agentmetry/internal/retention"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type RetentionServer struct {
	agentmetryv1connect.UnimplementedAgentmetryRetentionServiceHandler
	service *retention.Service
}

func NewRetention(service *retention.Service) (string, http.Handler) {
	return agentmetryv1connect.NewAgentmetryRetentionServiceHandler(&RetentionServer{service: service})
}

func (server *RetentionServer) GetRetentionStatus(ctx context.Context, _ *connect.Request[v1.GetRetentionStatusRequest]) (*connect.Response[v1.GetRetentionStatusResponse], error) {
	policy, updatedAt, err := server.service.Policy(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	operations, err := server.service.Operations(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	response := &v1.GetRetentionStatusResponse{Policy: policyMessage(policy, updatedAt)}
	cycles, err := server.service.Cycles(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	for _, cycle := range cycles {
		item := &v1.RetentionCycle{Id: cycle.ID, Status: string(cycle.Status), StartedAt: timestamppb.New(cycle.StartedAt),
			EvaluatedAt: timestamppb.New(cycle.EvaluatedAt), CohortSize: cycle.CohortSize, Error: cycle.Error,
			CohortUnavailableReason: cycle.CohortUnavailableReason, PendingChildren: cycle.PendingChildren,
			RunningChildren: cycle.RunningChildren, CompletedChildren: cycle.CompletedChildren,
			FailedChildren: cycle.FailedChildren, CancelledChildren: cycle.CancelledChildren}
		if cycle.CompletedAt != nil {
			item.CompletedAt = timestamppb.New(*cycle.CompletedAt)
		}
		response.Cycles = append(response.Cycles, item)
	}
	for _, operation := range operations {
		item := &v1.RetentionOperation{Id: operation.ID, Kind: string(operation.Kind), Status: string(operation.Status),
			Phase: operation.Phase, RequestedAt: timestamppb.New(operation.RequestedAt), Error: operation.Error,
			AffectedExports: operation.AffectedExports, AffectedSegments: operation.AffectedSegments}
		if operation.EvaluatedAt != nil {
			item.EvaluatedAt = timestamppb.New(*operation.EvaluatedAt)
		}
		if operation.CompletedAt != nil {
			item.CompletedAt = timestamppb.New(*operation.CompletedAt)
		}
		response.Operations = append(response.Operations, item)
	}
	return connect.NewResponse(response), nil
}

func (server *RetentionServer) UpdateRetentionPolicy(ctx context.Context, request *connect.Request[v1.UpdateRetentionPolicyRequest]) (*connect.Response[v1.UpdateRetentionPolicyResponse], error) {
	if request.Msg.Enabled && (request.Msg.ArchiveDays == nil || request.Msg.DeleteDays == nil) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("archive_days and delete_days are required when retention is enabled"))
	}
	if !request.Msg.Enabled && (request.Msg.ArchiveDays != nil || request.Msg.DeleteDays != nil) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("archive_days and delete_days must be omitted when retention is disabled"))
	}
	policy, err := server.service.UpdatePolicy(ctx, request.Msg.Enabled, int(request.Msg.GetArchiveDays()), int(request.Msg.GetDeleteDays()))
	if err != nil {
		return nil, retentionConnectError(err)
	}
	_, updatedAt, err := server.service.Policy(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&v1.UpdateRetentionPolicyResponse{Policy: policyMessage(policy, updatedAt)}), nil
}

func (server *RetentionServer) ListArchiveSegments(ctx context.Context, request *connect.Request[v1.ListArchiveSegmentsRequest]) (*connect.Response[v1.ListArchiveSegmentsResponse], error) {
	segments, next, err := server.service.Segments(ctx, request.Msg.PageToken, int(request.Msg.PageSize))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	response := &v1.ListArchiveSegmentsResponse{NextPageToken: next}
	for _, segment := range segments {
		item := &v1.ArchiveSegment{Id: segment.ID, MinReceivedAt: timestamppb.New(segment.MinReceivedAt),
			MaxReceivedAt: timestamppb.New(segment.MaxReceivedAt), ExportCount: int64(segment.ExportCount),
			OriginalBytes: segment.OriginalBytes, StoredBytes: segment.StoredBytes,
			PayloadIntegrity: string(segment.PayloadIntegrity), MetadataIntegrity: string(segment.MetadataIntegrity),
			ScheduleUnavailableReason: segment.ScheduleUnavailable, IntegrityError: segment.IntegrityError}
		if segment.ScheduledDeleteAt != nil {
			item.ScheduledDeleteAt = timestamppb.New(*segment.ScheduledDeleteAt)
		}
		response.Segments = append(response.Segments, item)
	}
	return connect.NewResponse(response), nil
}

func (server *RetentionServer) GetRetentionCapacity(ctx context.Context, _ *connect.Request[v1.GetRetentionCapacityRequest]) (*connect.Response[v1.GetRetentionCapacityResponse], error) {
	report, err := server.service.Capacity(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	response := &v1.GetRetentionCapacityResponse{ObservedAt: timestamppb.New(report.ObservedAt), DatabaseBytes: report.DatabaseBytes,
		DatabaseUnusedBytes: report.DatabaseUnusedBytes, WalBytes: report.WALBytes, ArchiveBytes: report.ArchiveBytes,
		UnavailableReason: report.UnavailableReason, TotalAllocatedBytes: report.TotalAllocatedBytes,
		ArchiveAllocatedBytes: report.ArchiveAllocatedBytes, StagingAllocatedBytes: report.StagingAllocatedBytes,
		ActiveRawBytes: report.ActiveRawBytes, ObservationBytes: report.ObservationBytes,
		QueryProjectionBytes: report.QueryProjectionBytes, EstimatedArchivePeakBytes: report.EstimatedArchivePeakBytes,
		EstimatedRestorePeakBytes: report.EstimatedRestorePeakBytes, Warning: report.Warning}
	if report.FilesystemAvailable {
		response.FilesystemFreeBytes = &report.FilesystemFreeBytes
	}
	return connect.NewResponse(response), nil
}

func (server *RetentionServer) RestoreArchive(ctx context.Context, request *connect.Request[v1.RestoreArchiveRequest]) (*connect.Response[v1.RestoreArchiveResponse], error) {
	var err error
	var scope retention.RestoreScope
	switch value := request.Msg.Scope.(type) {
	case *v1.RestoreArchiveRequest_SegmentId:
		scope, err = retention.SegmentScope(value.SegmentId)
	case *v1.RestoreArchiveRequest_ReceiveTime:
		if value.ReceiveTime == nil || value.ReceiveTime.Start == nil || value.ReceiveTime.End == nil || value.ReceiveTime.Start.CheckValid() != nil || value.ReceiveTime.End.CheckValid() != nil {
			err = retention.ErrInvalidScope
		} else {
			scope, err = retention.PeriodScope(value.ReceiveTime.Start.AsTime(), value.ReceiveTime.End.AsTime())
		}
	default:
		err = retention.ErrInvalidScope
	}
	if err != nil {
		return nil, retentionConnectError(err)
	}
	classification, err := server.service.ClassifyRestore(ctx, scope)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	response := &v1.RestoreArchiveResponse{Result: classification.Result, CurrentSegmentIds: classification.CurrentSegmentIDs,
		ActiveMatches: classification.ActiveMatches, ArchivedMatches: classification.ArchivedMatches, DeletedMatches: classification.DeletedMatches}
	if scope.Kind == retention.RestoreBySegment && request.Msg.HoldDays == nil {
		if classification.Result == string(retention.SegmentCurrent) {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("hold_days is required to restore a current segment"))
		}
		return connect.NewResponse(response), nil
	}
	if scope.Kind == retention.RestoreBySegment && classification.Result != string(retention.SegmentCurrent) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("hold_days must be omitted for a terminal or unknown segment lookup"))
	}
	if request.Msg.HoldDays == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("hold_days is required for a current segment or receive-time restore"))
	}
	hold, err := retention.NewHold(int(request.Msg.GetHoldDays()))
	if err != nil {
		return nil, retentionConnectError(err)
	}
	operationID, finalClassification, err := server.service.RequestRestore(ctx, scope, hold)
	if err != nil {
		return nil, retentionConnectError(err)
	}
	response.Result = finalClassification.Result
	response.CurrentSegmentIds = finalClassification.CurrentSegmentIDs
	response.ActiveMatches = finalClassification.ActiveMatches
	response.ArchivedMatches = finalClassification.ArchivedMatches
	response.DeletedMatches = finalClassification.DeletedMatches
	response.Accepted, response.OperationId = operationID != "", operationID
	return connect.NewResponse(response), nil
}

func policyMessage(policy retention.Policy, updatedAt time.Time) *v1.RetentionPolicy {
	message := &v1.RetentionPolicy{Enabled: policy.Enabled, Revision: policy.Revision, UpdatedAt: timestamppb.New(updatedAt)}
	if policy.Enabled {
		archiveDays, deleteDays := int32(policy.ArchiveAfter), int32(policy.DeleteAfter)
		message.ArchiveDays, message.DeleteDays = &archiveDays, &deleteDays
	}
	return message
}

func retentionConnectError(err error) error {
	code := connect.CodeFailedPrecondition
	if errors.Is(err, retention.ErrInvalidPolicy) || errors.Is(err, retention.ErrInvalidHold) || errors.Is(err, retention.ErrInvalidScope) {
		code = connect.CodeInvalidArgument
	}
	if errors.Is(err, retention.ErrInvalidArchive) || errors.Is(err, retention.ErrArchivePayload) || errors.Is(err, retention.ErrArchiveMetadata) {
		code = connect.CodeDataLoss
	}
	if errors.Is(err, retention.ErrArchiveFormat) {
		code = connect.CodeUnimplemented
	}
	if errors.Is(err, retention.ErrInsufficientCapacity) {
		code = connect.CodeResourceExhausted
	}
	if errors.Is(err, retention.ErrRestoreConflict) {
		code = connect.CodeAborted
	}
	return connect.NewError(code, err)
}
