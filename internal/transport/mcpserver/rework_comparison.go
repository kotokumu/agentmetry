package mcpserver

import (
	"context"
	"fmt"
	"time"

	"github.com/kotokumu/agentmetry/internal/query"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CompareReworkInput struct {
	Baseline RunReference `json:"baseline" jsonschema:"Required source and runId of the baseline run."`
	Current  RunReference `json:"current" jsonschema:"Required source and runId of the current run."`
}

type ReworkComparisonOutput struct {
	Status   string                        `json:"status"`
	Code     string                        `json:"code,omitempty"`
	Reason   string                        `json:"reason,omitempty"`
	Baseline ReworkComparisonSummaryOutput `json:"baseline"`
	Current  ReworkComparisonSummaryOutput `json:"current"`
	Rows     []query.ReworkComparisonRow   `json:"rows"`
}

type ReworkComparisonSummaryOutput struct {
	SourceID           string               `json:"sourceId"`
	SessionID          string               `json:"sessionId"`
	StartedAt          time.Time            `json:"startedAt"`
	EndedAt            time.Time            `json:"endedAt"`
	Coverage           query.ReworkCoverage `json:"coverage"`
	ProjectionCoverage string               `json:"projectionCoverage"`
	HarnessContext     HarnessContextOutput `json:"harnessContext"`
}

type HarnessContextOutput struct {
	Classification string                      `json:"classification"`
	Counts         HarnessEvidenceCountsOutput `json:"counts"`
	Identity       *HarnessIdentityOutput      `json:"identity,omitempty"`
}

type HarnessEvidenceCountsOutput struct {
	EligibleRecords    int64 `json:"eligibleRecords"`
	ReportedRecords    int64 `json:"reportedRecords"`
	UnreportedRecords  int64 `json:"unreportedRecords"`
	InvalidRecords     int64 `json:"invalidRecords"`
	DistinctIdentities int64 `json:"distinctIdentities"`
}

type HarnessIdentityOutput struct {
	Scope       string `json:"scope"`
	Fingerprint string `json:"fingerprint"`
	Label       string `json:"label,omitempty"`
}

func (service *Service) compareRework(ctx context.Context, _ *mcp.CallToolRequest, input CompareReworkInput) (*mcp.CallToolResult, ReworkComparisonOutput, error) {
	baseline, err := query.NewConversationIdentity(input.Baseline.Source, input.Baseline.RunID)
	if err != nil {
		return nil, ReworkComparisonOutput{}, err
	}
	current, err := query.NewConversationIdentity(input.Current.Source, input.Current.RunID)
	if err != nil {
		return nil, ReworkComparisonOutput{}, err
	}
	reader, ok := service.summaryReader.(query.ReworkComparisonReader)
	if !ok {
		return nil, ReworkComparisonOutput{}, fmt.Errorf("rework comparison is unavailable")
	}
	report, err := reader.CompareRework(ctx, query.ReworkComparisonPair{Baseline: baseline, Current: current})
	if err != nil {
		return nil, ReworkComparisonOutput{}, err
	}
	before, err := mapReworkComparisonSummary(report.Baseline)
	if err != nil {
		return nil, ReworkComparisonOutput{}, err
	}
	after, err := mapReworkComparisonSummary(report.Current)
	if err != nil {
		return nil, ReworkComparisonOutput{}, err
	}
	return nil, ReworkComparisonOutput{Status: report.Status, Code: report.Code, Reason: report.Reason, Baseline: before, Current: after, Rows: append([]query.ReworkComparisonRow{}, report.Rows...)}, nil
}

func mapReworkComparisonSummary(summary query.ReworkComparisonSummary) (ReworkComparisonSummaryOutput, error) {
	view, err := query.InspectHarnessContext(summary.Harness)
	if err != nil {
		return ReworkComparisonSummaryOutput{}, fmt.Errorf("map comparison harness context: %w", err)
	}
	counts := view.Counts
	harness := HarnessContextOutput{Classification: string(view.Classification), Counts: HarnessEvidenceCountsOutput{
		EligibleRecords: counts.EligibleRecords, ReportedRecords: counts.ReportedRecords, UnreportedRecords: counts.UnreportedRecords,
		InvalidRecords: counts.InvalidRecords, DistinctIdentities: counts.DistinctIdentities,
	}}
	if view.Identity != nil {
		harness.Identity = &HarnessIdentityOutput{Scope: view.Identity.Scope, Fingerprint: view.Identity.Fingerprint, Label: view.Identity.Label}
	}
	return ReworkComparisonSummaryOutput{SourceID: summary.SourceID, SessionID: summary.SessionID, StartedAt: summary.StartedAt, EndedAt: summary.EndedAt,
		Coverage: summary.Coverage, ProjectionCoverage: summary.ProjectionCoverage, HarnessContext: harness}, nil
}
