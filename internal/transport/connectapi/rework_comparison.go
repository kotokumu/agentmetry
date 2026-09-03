package connectapi

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	v1 "github.com/kotokumu/agentmetry/gen/agentmetry/v1"
	"github.com/kotokumu/agentmetry/internal/query"
)

func (server *Server) CompareRework(ctx context.Context, request *connect.Request[v1.CompareReworkRequest]) (*connect.Response[v1.CompareReworkResponse], error) {
	baseline, err := query.NewConversationIdentity(request.Msg.GetBaseline().GetSourceId(), request.Msg.GetBaseline().GetSessionId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	current, err := query.NewConversationIdentity(request.Msg.GetCurrent().GetSourceId(), request.Msg.GetCurrent().GetSessionId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	reader, ok := server.reader.(query.ReworkComparisonReader)
	if !ok {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("rework comparison is unavailable"))
	}
	report, err := reader.CompareRework(ctx, query.ReworkComparisonPair{Baseline: baseline, Current: current})
	if errors.Is(err, query.ErrConversationNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	response, err := mapReworkComparison(report)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(response), nil
}

func mapReworkComparison(report query.ReworkComparison) (*v1.CompareReworkResponse, error) {
	baseline, err := mapReworkComparisonSummary(report.Baseline)
	if err != nil {
		return nil, fmt.Errorf("map baseline comparison: %w", err)
	}
	current, err := mapReworkComparisonSummary(report.Current)
	if err != nil {
		return nil, fmt.Errorf("map current comparison: %w", err)
	}
	result := &v1.CompareReworkResponse{Status: report.Status, Code: report.Code, Reason: report.Reason, Baseline: baseline, Current: current}
	for _, row := range report.Rows {
		result.Rows = append(result.Rows, &v1.ReworkComparisonRow{
			Id: row.ID, Unit: row.Unit, Availability: row.Availability,
			Baseline: mapReworkComparisonValue(row.Baseline), Current: mapReworkComparisonValue(row.Current), Delta: row.Delta,
		})
	}
	return result, nil
}

func mapReworkComparisonSummary(summary query.ReworkComparisonSummary) (*v1.ReworkComparisonSummary, error) {
	harness, err := mapHarnessContext(summary.Harness)
	if err != nil {
		return nil, err
	}
	return &v1.ReworkComparisonSummary{
		SourceId: summary.SourceID, SessionId: summary.SessionID, StartedAt: timestamp(summary.StartedAt), EndedAt: timestamp(summary.EndedAt),
		Coverage: mapReworkCoverage(summary.Coverage), ProjectionCoverage: summary.ProjectionCoverage, HarnessContext: harness,
	}, nil
}

func mapReworkComparisonValue(value query.ReworkComparisonValue) *v1.ReworkComparisonValue {
	return &v1.ReworkComparisonValue{Availability: value.Availability, Reason: value.Reason, Numerator: value.Numerator, Denominator: value.Denominator, Value: value.Value}
}

func mapReworkCoverage(coverage query.ReworkCoverage) *v1.ReworkCoverage {
	return &v1.ReworkCoverage{
		ActivityCoverage: coverage.ActivityCoverage, CanonicalEvents: coverage.CanonicalEvents,
		ClassifiedEvents: coverage.ClassifiedEvents, KnownOutcomes: coverage.KnownOutcomes,
		ValidationAttempts: coverage.ValidationAttempts, FingerprintedFailures: coverage.FingerprintedFailures,
		IdentifiedValidationAttempts: coverage.IdentifiedValidationAttempts, IdBackedValidationAttempts: coverage.IDBackedValidationAttempts,
		UncorrelatedValidationObservations: coverage.UncorrelatedValidationObservations, ConflictingAttemptObservations: coverage.ConflictingAttemptObservations,
		MergedValidationAttempts: coverage.MergedValidationAttempts, AmbiguousFailureAttempts: coverage.AmbiguousFailureAttempts,
	}
}
