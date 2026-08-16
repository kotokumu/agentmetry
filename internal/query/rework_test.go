package query

import (
	"math"
	"testing"
	"time"

	"github.com/theoden9014/agentmetry/internal/canonical"
)

func TestCanonicalizeActivityNormalizesCodexAndClaudeToolEvidence(t *testing.T) {
	tests := []struct {
		name      string
		activity  Activity
		operation canonical.Operation
		file      string
		command   string
		success   *bool
	}{
		{
			name: "codex stringified command and exit code",
			activity: Activity{
				Source: "codex", RunID: "run-1", Kind: canonical.ActivityTool, ToolName: "exec_command",
				Attributes: map[string]any{
					"arguments": `{"cmd":"go   test ./..."}`,
					"output":    `{"exit_code":1}`,
				},
			},
			operation: canonical.OperationTest,
			command:   "go test ./...",
			success:   boolPointer(false),
		},
		{
			name: "claude file edit",
			activity: Activity{
				Source: "claude", RunID: "run-2", Kind: canonical.ActivityTool, ToolName: "Write", Status: "OK",
				Attributes: map[string]any{"file_path": "internal/query/rework.go"},
			},
			operation: canonical.OperationEdit,
			file:      "internal/query/rework.go",
			success:   boolPointer(true),
		},
		{
			name: "patch header supplies file target without inventing outcome",
			activity: Activity{
				Source: "codex", RunID: "run-3", Kind: canonical.ActivityTool, ToolName: "apply_patch",
				Attributes: map[string]any{"patch": "*** Begin Patch\n*** Update File: internal/query/rework.go\n@@"},
			},
			operation: canonical.OperationEdit,
			file:      "internal/query/rework.go",
		},
		{
			name:      "build command",
			activity:  Activity{ToolName: "exec_command", Attributes: map[string]any{"command": "go build ./..."}},
			operation: canonical.OperationBuild,
			command:   "go build ./...",
		},
		{
			name:      "lint command",
			activity:  Activity{ToolName: "exec_command", Attributes: map[string]any{"command": "go vet ./..."}},
			operation: canonical.OperationLint,
			command:   "go vet ./...",
		},
		{
			name:      "terminal stdin remains execution",
			activity:  Activity{ToolName: "write_stdin"},
			operation: canonical.OperationExecute,
		},
		{
			name:      "malformed optional arguments remain unclassified",
			activity:  Activity{Attributes: map[string]any{"arguments": "{not-json"}},
			operation: canonical.OperationOther,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event := CanonicalizeActivity(test.activity)
			if event.Operation != test.operation || event.Target.File != test.file || event.Target.Command != test.command {
				t.Fatalf("unexpected canonical event: %#v", event)
			}
			if !equalOptionalBool(event.Success, test.success) {
				t.Fatalf("success = %#v, want %#v", event.Success, test.success)
			}
		})
	}
}

func TestAnalyzeReworkCalculatesEvidenceBackedSessionMetrics(t *testing.T) {
	start := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	activities := []Activity{
		toolActivity(start, 0, 2, "test-fail", "agent-1", "exec_command", map[string]any{"command": "go test ./...", "exit_code": 1}, 10),
		toolActivity(start, 3, 5, "edit-1", "agent-1", "apply_patch", map[string]any{"file_path": "main.go", "success": true}, 20),
		{Source: "codex", RunID: "run-1", TraceID: "trace-1", SpanID: "unrelated-agent", Name: "gen_ai.response.completed", Kind: canonical.ActivityResponse, AgentID: "agent-2", StartedAt: start.Add(3 * time.Second), EndedAt: start.Add(20 * time.Second), ObservedAt: start.Add(20 * time.Second), Tokens: canonical.TokenUsage{Input: 100, Presence: canonical.TokenPresence{Output: true}}, ContributesToTotal: true},
		toolActivity(start, 4, 4, "edit-nested", "agent-1", "apply_patch", map[string]any{"file_path": "main.go"}, 99),
		toolActivity(start, 6, 8, "test-retry", "agent-1", "exec_command", map[string]any{"command": "go   test ./...", "exit_code": 0}, 30),
		toolActivity(start, 9, 10, "api-fail", "agent-1", "", map[string]any{"event.name": "gen_ai.model.error", "model": "gpt-5", "success": false}, 7),
		toolActivity(start, 11, 12, "api-retry", "agent-1", "", map[string]any{"event.name": "gen_ai.model.request", "model": "gpt-5", "success": true}, 8),
	}
	activities[3].ContributesToTotal = false
	summary := Session{SourceID: "codex", ID: "run-1", ActivityCount: int64(len(activities) + 1)}

	report := AnalyzeRework(summary, activities)

	if report.ValidationFailures != 1 || report.FailFixRetryCycles != 1 {
		t.Fatalf("unexpected failures/cycles: %#v", report)
	}
	if report.ReworkDuration != 6*time.Second || report.ReworkTokens.Total() != 60 {
		t.Fatalf("unexpected rework effort: %#v", report)
	}
	if report.ToolAttemptsWithOutcome != 5 || report.ToolFailures != 2 || report.ToolFailureRate == nil || math.Abs(*report.ToolFailureRate-0.4) > 0.000001 {
		t.Fatalf("unexpected tool failure rate: %#v", report)
	}
	if report.APIRetryWaste.Attempts != 1 || report.APIRetryWaste.Duration != time.Second || report.APIRetryWaste.Tokens.Total() != 7 {
		t.Fatalf("unexpected API retry waste: %#v", report.APIRetryWaste)
	}
	if report.RepeatedCommands != 1 || report.ReeditedFiles != 1 {
		t.Fatalf("unexpected repeat metrics: %#v", report)
	}
	if len(report.Cycles) != 1 || len(report.Cycles[0].Evidence) != 4 {
		t.Fatalf("unexpected cycle evidence: %#v", report.Cycles)
	}
	if report.Coverage.ActivityCoverage != ActivityCoveragePartial || report.Coverage.CanonicalEvents != int64(len(activities)) || report.Coverage.KnownOutcomes != 5 {
		t.Fatalf("unexpected coverage: %#v", report.Coverage)
	}
	if report.Capabilities.ChangeRevert.State != CapabilityUnavailable || report.Capabilities.CrossAgentOverlap.State != CapabilityUnavailable {
		t.Fatalf("unsupported metrics were not explicit: %#v", report.Capabilities)
	}
}

func TestAnalyzeReworkIsDeterministicAndDoesNotMatchUnrelatedRetries(t *testing.T) {
	start := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	activities := []Activity{
		toolActivity(start, 4, 5, "other-command", "main", "exec_command", map[string]any{"command": "go vet ./...", "exit_code": 0}, 0),
		toolActivity(start, 2, 3, "edit", "main", "apply_patch", map[string]any{"file_path": "main.go", "success": true}, 0),
		toolActivity(start, 0, 1, "failed-test", "main", "exec_command", map[string]any{"command": "go test ./...", "exit_code": 1}, 0),
	}

	report := AnalyzeRework(Session{ActivityCount: 3}, activities)

	if report.FailFixRetryCycles != 0 || report.ReworkDuration != 0 {
		t.Fatalf("unrelated operation formed a cycle: %#v", report)
	}
}

func TestAnalyzeReworkClipsObservedEffortAtTheRetryBoundary(t *testing.T) {
	start := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	activities := []Activity{
		toolActivity(start, 0, 1, "failed", "main", "exec_command", map[string]any{"command": "go test ./...", "exit_code": 1}, 0),
		toolActivity(start, 2, 10, "long-edit", "main", "apply_patch", map[string]any{"file_path": "main.go", "success": true}, 0),
		toolActivity(start, 4, 5, "retry", "main", "exec_command", map[string]any{"command": "go test ./...", "exit_code": 0}, 0),
	}

	report := AnalyzeRework(Session{ActivityCount: 3}, activities)

	if report.ReworkDuration != 4*time.Second {
		t.Fatalf("rework duration = %s, want effort clipped to retry boundary", report.ReworkDuration)
	}
}

func toolActivity(start time.Time, fromSecond, toSecond int, spanID, agentID, toolName string, attributes map[string]any, tokens int64) Activity {
	return Activity{
		Source: "codex", RunID: "run-1", TraceID: "trace-1", SpanID: spanID,
		Kind: canonical.ActivityTool, ToolName: toolName, Name: toolName, AgentID: agentID,
		StartedAt: start.Add(time.Duration(fromSecond) * time.Second), EndedAt: start.Add(time.Duration(toSecond) * time.Second),
		ObservedAt: start.Add(time.Duration(toSecond) * time.Second), Attributes: attributes,
		Tokens:             canonical.TokenUsage{Input: tokens, Presence: canonical.TokenPresence{Output: true}},
		ContributesToTotal: true,
	}
}

func boolPointer(value bool) *bool { return &value }

func equalOptionalBool(first, second *bool) bool {
	if first == nil || second == nil {
		return first == nil && second == nil
	}
	return *first == *second
}
