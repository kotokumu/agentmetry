package query

import (
	"math"
	"testing"
	"time"

	"github.com/kotokumu/agentmetry/internal/canonical"
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
	if report.TotalAgentEffort != 25*time.Second || report.ReworkAgentEffortRate == nil || math.Abs(*report.ReworkAgentEffortRate-0.24) > 0.000001 {
		t.Fatalf("unexpected normalized rework effort: %#v", report)
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

func TestAnalyzeReworkDetectsRecurringFailureEpisodeAndResolutionEffort(t *testing.T) {
	start := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	activities := []Activity{
		toolActivity(start, 0, 1, "fail-1", "agent-1", "exec_command", map[string]any{"command": "go test ./...", "exit_code": 1, "stderr": "--- FAIL: TestSave (0.01s)\n/Users/me/repo/store_test.go:42: expected 2, got 1"}, 5),
		toolActivity(start, 2, 3, "edit-1", "agent-1", "apply_patch", map[string]any{"file_path": "store.go", "success": true}, 7),
		toolActivity(start, 4, 5, "fail-2", "agent-1", "exec_command", map[string]any{"command": "go test ./...", "exit_code": 1, "stderr": "--- FAIL: TestSave (0.02s)\n/tmp/repo/store_test.go:98: expected 2, got 1"}, 11),
		toolActivity(start, 6, 7, "edit-2", "agent-1", "apply_patch", map[string]any{"file_path": "store.go", "success": true}, 13),
		toolActivity(start, 8, 9, "fail-3", "agent-1", "exec_command", map[string]any{"command": "go test ./...", "exit_code": 1, "stderr": "--- FAIL: TestSave (0.03s)\n/var/repo/store_test.go:7: expected 2, got 1"}, 17),
		toolActivity(start, 10, 11, "pass", "agent-1", "exec_command", map[string]any{"command": "go test ./...", "exit_code": 0}, 19),
	}

	report := AnalyzeRework(Session{ActivityCount: int64(len(activities))}, activities)

	if report.RecurringFailureLoops != 1 || report.RepeatedFailureAttempts != 3 {
		t.Fatalf("recurring failures = %d attempts across %d loops, want 3 across 1", report.RepeatedFailureAttempts, report.RecurringFailureLoops)
	}
	if report.ResolvedFailureLoops != 1 || report.UnresolvedFailureLoops != 0 {
		t.Fatalf("resolution counts = %d resolved/%d unresolved", report.ResolvedFailureLoops, report.UnresolvedFailureLoops)
	}
	if report.FailureResolutionDuration != 11*time.Second || report.FailureResolutionTokens.Total() != 72 {
		t.Fatalf("resolution effort = %s/%d tokens, want 11s/72", report.FailureResolutionDuration, report.FailureResolutionTokens.Total())
	}
	if report.ValidationAttemptsWithOutcome != 4 || report.FirstPassEligibleValidations != 1 || report.FirstPassSuccesses != 0 || report.FirstPassSuccessRate == nil || *report.FirstPassSuccessRate != 0 {
		t.Fatalf("unexpected first-pass metrics: %#v", report)
	}
	if report.Coverage.ValidationAttempts != 4 || report.Coverage.FingerprintedFailures != 3 || report.Coverage.IdentifiedValidationAttempts != 4 {
		t.Fatalf("unexpected recurrence coverage: %#v", report.Coverage)
	}
	if len(report.FailureEpisodes) != 1 || report.FailureEpisodes[0].ValidationFingerprint == "" || report.FailureEpisodes[0].AgentID != "agent-1" || len(report.FailureEpisodes[0].ErrorFingerprints) != 1 || report.FailureEpisodes[0].FailureAttempts != 3 || !report.FailureEpisodes[0].Resolved || report.FailureEpisodes[0].TraceID != "trace-1" || report.FailureEpisodes[0].SpanID != "fail-1" {
		t.Fatalf("unexpected recurring failure detail: %#v", report.FailureEpisodes)
	}
}

func TestAnalyzeReworkSeparatesDifferentErrorsAndReportsUnresolvedLoops(t *testing.T) {
	start := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	activities := []Activity{
		toolActivity(start, 0, 1, "fail-x", "main", "exec_command", map[string]any{"command": "go test ./...", "exit_code": 1, "stderr": "FAIL TestSave: expected 2, got 1"}, 3),
		toolActivity(start, 2, 3, "fail-y", "main", "exec_command", map[string]any{"command": "go test ./...", "exit_code": 1, "stderr": "FAIL TestLoad: unexpected EOF"}, 5),
		toolActivity(start, 4, 5, "pass", "main", "exec_command", map[string]any{"command": "go test ./...", "exit_code": 0}, 7),
		toolActivity(start, 6, 7, "lint-fail-1", "main", "exec_command", map[string]any{"command": "npm run lint", "exit_code": 1, "stderr": "src/a.ts:10:2 no-unused-vars"}, 11),
		toolActivity(start, 8, 9, "lint-fail-2", "main", "exec_command", map[string]any{"command": "npm run lint", "exit_code": 1, "stderr": "src/a.ts:27:9 no-unused-vars"}, 13),
	}

	report := AnalyzeRework(Session{ActivityCount: int64(len(activities))}, activities)

	if report.RecurringFailureLoops != 1 || report.RepeatedFailureAttempts != 2 || report.ResolvedFailureLoops != 0 || report.UnresolvedFailureLoops != 1 {
		t.Fatalf("unexpected recurring-loop metrics: %#v", report)
	}
	if report.FailureResolutionDuration != 0 || report.FailureResolutionTokens.AnyReported() {
		t.Fatalf("unresolved loops must not be reported as resolution effort: %#v", report)
	}
}

func TestAnalyzeReworkCountsEveryFailureInAnEpisodeWithARecurringFingerprint(t *testing.T) {
	start := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	activities := []Activity{
		toolActivity(start, 0, 1, "fail-x-1", "main", "exec_command", map[string]any{"command": "go test ./...", "exit_code": 1, "stderr": "FAIL TestSave: expected 2, got 1"}, 0),
		toolActivity(start, 2, 3, "fail-y", "main", "exec_command", map[string]any{"command": "go test ./...", "exit_code": 1, "stderr": "FAIL TestLoad: unexpected EOF"}, 0),
		toolActivity(start, 4, 5, "fail-x-2", "main", "exec_command", map[string]any{"command": "go test ./...", "exit_code": 1, "stderr": "FAIL TestSave: expected 2, got 1"}, 0),
		toolActivity(start, 6, 7, "pass", "main", "exec_command", map[string]any{"command": "go test ./...", "exit_code": 0}, 0),
	}

	report := AnalyzeRework(Session{ActivityCount: int64(len(activities))}, activities)

	if report.RecurringFailureLoops != 1 || report.RepeatedFailureAttempts != 3 {
		t.Fatalf("recurring episode = %d attempts across %d loops, want all 3 failures in 1 loop", report.RepeatedFailureAttempts, report.RecurringFailureLoops)
	}
}

func TestAnalyzeReworkDeduplicatesSpanAndLogForOneValidationAttempt(t *testing.T) {
	start := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	span := toolActivity(start, 0, 1, "tool-span", "main", "exec_command", map[string]any{"command": "go test ./...", "exit_code": 1}, 0)
	span.Signal = canonical.SignalTrace
	span.Attributes["gen_ai.tool.call.id"] = "call-1"
	log := toolActivity(start, 0, 1, "tool-log", "main", "", map[string]any{"exit_code": 1, "stderr": "FAIL TestSave: expected 2, got 1", "gen_ai.tool.call.id": "call-1"}, 0)
	log.Signal = canonical.SignalLog
	log.Name = "gen_ai.tool_result"
	log.RelatedTraceID = span.TraceID
	log.RelatedSpanID = span.SpanID
	activities := []Activity{span, log, toolActivity(start, 2, 3, "pass", "main", "exec_command", map[string]any{"command": "go test ./...", "exit_code": 0}, 0)}

	report := AnalyzeRework(Session{ActivityCount: int64(len(activities))}, activities)

	if report.ValidationFailures != 1 || report.ValidationAttemptsWithOutcome != 2 || report.RecurringFailureLoops != 0 {
		t.Fatalf("duplicate observations became logical attempts: %#v", report)
	}
	if report.Coverage.IDBackedValidationAttempts != 2 || report.Coverage.MergedValidationAttempts != 1 || report.Coverage.UncorrelatedValidationObservations != 0 {
		t.Fatalf("unexpected attempt correlation coverage: %#v", report.Coverage)
	}
}

func TestAnalyzeReworkDoesNotConfirmRecurrenceFromUncorrelatedLogs(t *testing.T) {
	start := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	first := toolActivity(start, 0, 1, "parent-span", "main", "exec_command", map[string]any{"command": "go test ./...", "exit_code": 1, "stderr": "same failure"}, 0)
	second := toolActivity(start, 2, 3, "parent-span", "main", "exec_command", map[string]any{"command": "go test ./...", "exit_code": 1, "stderr": "same failure"}, 0)
	first.Signal, second.Signal = canonical.SignalLog, canonical.SignalLog
	report := AnalyzeRework(Session{ActivityCount: 3}, []Activity{first, second, toolActivity(start, 4, 5, "pass", "main", "exec_command", map[string]any{"command": "go test ./...", "exit_code": 0}, 0)})

	if report.ValidationFailures != 2 || report.RecurringFailureLoops != 0 || report.RepeatedFailureAttempts != 0 {
		t.Fatalf("uncorrelated logs became confirmed recurrence: %#v", report)
	}
	if report.Coverage.UncorrelatedValidationObservations != 2 {
		t.Fatalf("uncorrelated evidence coverage = %#v", report.Coverage)
	}
}

func TestAnalyzeReworkNamespacesToolCallIdentityAndValidationWorkingDirectory(t *testing.T) {
	start := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	firstAgent := toolActivity(start, 0, 1, "a", "agent-a", "exec_command", map[string]any{"command": "go test ./...", "cwd": "/repo/a", "exit_code": 1, "stderr": "same failure", "gen_ai.tool.call.id": "call-1"}, 0)
	secondAgent := toolActivity(start, 2, 3, "b", "agent-b", "exec_command", map[string]any{"command": "go test ./...", "cwd": "/repo/b", "exit_code": 1, "stderr": "same failure", "gen_ai.tool.call.id": "call-1"}, 0)
	report := AnalyzeRework(Session{ActivityCount: 2}, []Activity{firstAgent, secondAgent})
	if report.ValidationFailures != 2 || report.RecurringFailureLoops != 0 {
		t.Fatalf("tool-call ID collided across agent namespaces: %#v", report)
	}

	cwdA := toolActivity(start, 4, 5, "cwd-a", "main", "exec_command", map[string]any{"command": "go test ./...", "cwd": "/repo/a", "exit_code": 1, "stderr": "same failure"}, 0)
	cwdB := toolActivity(start, 6, 7, "cwd-b", "main", "exec_command", map[string]any{"command": "go test ./...", "cwd": "/repo/b", "exit_code": 1, "stderr": "same failure"}, 0)
	passB := toolActivity(start, 8, 9, "pass-b", "main", "exec_command", map[string]any{"command": "go test ./...", "cwd": "/repo/b", "exit_code": 0}, 0)
	cwdReport := AnalyzeRework(Session{ActivityCount: 3}, []Activity{cwdA, cwdB, passB})
	if cwdReport.RecurringFailureLoops != 0 {
		t.Fatalf("different working directories formed one episode: %#v", cwdReport)
	}
}

func TestProjectValidationAttemptsUpgradesSpecificityAndReportsConflicts(t *testing.T) {
	failed := false
	projection := projectValidationAttempts([]canonical.Event{
		{AttemptID: "attempt-1", Operation: canonical.OperationExecute, AgentID: "main", Target: canonical.EventTarget{Command: "custom validator"}},
		{AttemptID: "attempt-1", Operation: canonical.OperationTest, AgentID: "main", Target: canonical.EventTarget{Command: "custom validator"}, Success: &failed},
		{AttemptID: "attempt-2", Operation: canonical.OperationTest, AgentID: "main", Target: canonical.EventTarget{Command: "go test ./..."}, Success: &failed},
		{AttemptID: "attempt-2", Operation: canonical.OperationBuild, AgentID: "main", Target: canonical.EventTarget{Command: "go test ./..."}, Success: &failed},
	})
	if len(projection.attempts) != 3 || projection.attempts[0].Operation != canonical.OperationTest || projection.conflictingObservations != 1 {
		t.Fatalf("unexpected validation projection: %#v", projection)
	}
}

func TestProjectValidationAttemptsExcludesAmbiguousFailureEvidenceFromRecurrence(t *testing.T) {
	failed, passed := false, true
	projection := projectValidationAttempts([]canonical.Event{
		{AttemptID: "attempt-1", Operation: canonical.OperationTest, AgentID: "main", Target: canonical.EventTarget{Command: "go test ./..."}, Success: &failed, ErrorFingerprint: "generic"},
		{AttemptID: "attempt-1", Operation: canonical.OperationOther, AgentID: "main", Success: &failed, ErrorFingerprint: "detail-x"},
		{AttemptID: "attempt-2", Operation: canonical.OperationTest, AgentID: "main", Target: canonical.EventTarget{Command: "go test ./..."}, Success: &failed, ErrorFingerprint: "generic"},
		{AttemptID: "attempt-2", Operation: canonical.OperationOther, AgentID: "main", Success: &failed, ErrorFingerprint: "detail-y"},
		{AttemptID: "attempt-3", Operation: canonical.OperationTest, AgentID: "main", Target: canonical.EventTarget{Command: "go test ./..."}, Success: &passed},
	})
	if projection.ambiguousFailures != 2 || projection.attempts[0].ErrorFingerprint != "" || projection.attempts[1].ErrorFingerprint != "" {
		t.Fatalf("ambiguous failure evidence was not excluded: %#v", projection)
	}
	for _, episode := range detectFailureEpisodes(projection.attempts) {
		if episode.recurring() {
			t.Fatalf("ambiguous evidence formed recurring episode: %#v", episode)
		}
	}
}

func TestAnalyzeReworkRequiresCommandIdentityForEpisodeAndFirstPass(t *testing.T) {
	start := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	activities := []Activity{
		toolActivity(start, 0, 1, "fail-1", "main", "test_tool", map[string]any{"exit_code": 1, "stderr": "same error"}, 0),
		toolActivity(start, 2, 3, "fail-2", "main", "test_tool", map[string]any{"exit_code": 1, "stderr": "same error"}, 0),
	}

	report := AnalyzeRework(Session{ActivityCount: int64(len(activities))}, activities)

	if report.RecurringFailureLoops != 0 || report.FirstPassSuccessRate != nil || report.FirstPassEligibleValidations != 0 {
		t.Fatalf("commandless validation was assigned an identity: %#v", report)
	}
	if report.Coverage.IdentifiedValidationAttempts != 0 {
		t.Fatalf("commandless validation reported identity coverage: %#v", report.Coverage)
	}
}

func TestAnalyzeReworkFirstPassRateUsesFirstKnownOutcomePerValidationIdentity(t *testing.T) {
	start := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	activities := []Activity{
		toolActivity(start, 0, 1, "test-pass", "main", "exec_command", map[string]any{"command": "go test ./...", "exit_code": 0}, 0),
		toolActivity(start, 2, 3, "test-pass-again", "main", "exec_command", map[string]any{"command": "go test ./...", "exit_code": 0}, 0),
		toolActivity(start, 4, 5, "lint-fail", "main", "exec_command", map[string]any{"command": "go vet ./...", "exit_code": 1}, 0),
		toolActivity(start, 6, 7, "build-unknown", "main", "exec_command", map[string]any{"command": "go build ./..."}, 0),
	}

	report := AnalyzeRework(Session{ActivityCount: int64(len(activities))}, activities)

	if report.ValidationAttemptsWithOutcome != 3 || report.FirstPassEligibleValidations != 2 || report.FirstPassSuccesses != 1 || report.FirstPassSuccessRate == nil || *report.FirstPassSuccessRate != 0.5 {
		t.Fatalf("unexpected first-pass metrics: %#v", report)
	}
}

func TestCanonicalizeActivityOnlyFingerprintsObservedFailureEvidence(t *testing.T) {
	sameFailureA := CanonicalizeActivity(Activity{ToolName: "exec_command", Content: "FAIL TestSave", Attributes: map[string]any{
		"command": "go test ./...", "exit_code": 1, "stderr": "2026-08-17T10:15:31Z /Users/me/repo/store_test.go:42: expected 2, got 1 request 550e8400-e29b-41d4-a716-446655440000",
	}})
	sameFailureB := CanonicalizeActivity(Activity{ToolName: "exec_command", Attributes: map[string]any{
		"command": "go test ./...", "exit_code": 1, "output": map[string]any{"stderr": "2026-08-18T11:16:32Z /tmp/repo/store_test.go:98: expected 2, got 1 request 123e4567-e89b-12d3-a456-426614174000"},
	}})
	differentFailure := CanonicalizeActivity(Activity{ToolName: "exec_command", Attributes: map[string]any{
		"command": "go test ./...", "exit_code": 1, "stderr": "store_test.go:42: unexpected EOF",
	}})
	differentFile := CanonicalizeActivity(Activity{ToolName: "exec_command", Attributes: map[string]any{
		"command": "go test ./...", "exit_code": 1, "stderr": "/tmp/repo/load_test.go:42: expected 2, got 1",
	}})
	sameBasenameDifferentDirectory := CanonicalizeActivity(Activity{ToolName: "exec_command", Attributes: map[string]any{
		"command": "go test ./...", "exit_code": 1, "stderr": "/tmp/repo/other/store_test.go:42: expected 2, got 1 request 550e8400-e29b-41d4-a716-446655440000",
	}})
	missingEvidence := CanonicalizeActivity(Activity{ToolName: "exec_command", Attributes: map[string]any{
		"command": "go test ./...", "exit_code": 1, "output": `{"exit_code":1}`,
	}})

	if sameFailureA.ErrorFingerprint == "" || sameFailureA.ErrorFingerprint != sameFailureB.ErrorFingerprint {
		t.Fatalf("equivalent failures were not normalized: %q != %q (%q / %q)", sameFailureA.ErrorFingerprint, sameFailureB.ErrorFingerprint,
			normalizeFailureEvidence("2026-08-17T10:15:31Z /Users/me/repo/store_test.go:42: expected 2, got 1 request 550e8400-e29b-41d4-a716-446655440000"),
			normalizeFailureEvidence("2026-08-18T11:16:32Z /tmp/repo/store_test.go:98: expected 2, got 1 request 123e4567-e89b-12d3-a456-426614174000"))
	}
	if differentFailure.ErrorFingerprint == sameFailureA.ErrorFingerprint {
		t.Fatalf("different failures received the same fingerprint: %q", differentFailure.ErrorFingerprint)
	}
	if differentFile.ErrorFingerprint == sameFailureA.ErrorFingerprint {
		t.Fatalf("different file failures received the same fingerprint: %q", differentFile.ErrorFingerprint)
	}
	if sameBasenameDifferentDirectory.ErrorFingerprint == sameFailureA.ErrorFingerprint {
		t.Fatalf("same-basename failures in different directories collided: %q", sameBasenameDifferentDirectory.ErrorFingerprint)
	}
	if missingEvidence.ErrorFingerprint != "" {
		t.Fatalf("missing evidence received fingerprint %q", missingEvidence.ErrorFingerprint)
	}
}

func TestAnalyzeReworkMergesOverlappingEffortPerAgentAndCountsParallelAgents(t *testing.T) {
	start := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	activities := []Activity{
		toolActivity(start, 0, 10, "agent-1-outer", "agent-1", "read_file", map[string]any{"file_path": "main.go"}, 0),
		toolActivity(start, 2, 6, "agent-1-inner", "agent-1", "read_file", map[string]any{"file_path": "main.go"}, 0),
		toolActivity(start, 3, 8, "agent-2-parallel", "agent-2", "read_file", map[string]any{"file_path": "other.go"}, 0),
	}

	report := AnalyzeRework(Session{ActivityCount: int64(len(activities))}, activities)

	if report.TotalAgentEffort != 15*time.Second {
		t.Fatalf("total agent effort = %s, want 15s", report.TotalAgentEffort)
	}
	if report.ReworkAgentEffortRate == nil || *report.ReworkAgentEffortRate != 0 {
		t.Fatalf("rework effort rate = %#v, want observed zero", report.ReworkAgentEffortRate)
	}
}

func TestAnalyzeReworkIncludesSameAgentActivitySpanningTheRetryCycleWindow(t *testing.T) {
	start := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	activities := []Activity{
		{Source: "codex", RunID: "run-1", TraceID: "trace-1", SpanID: "parent", Name: "agent turn", AgentID: "agent-1", StartedAt: start, EndedAt: start.Add(10 * time.Second), ObservedAt: start.Add(10 * time.Second)},
		toolActivity(start, 1, 2, "failed", "agent-1", "exec_command", map[string]any{"command": "go test ./...", "exit_code": 1}, 0),
		toolActivity(start, 3, 4, "edit", "agent-1", "apply_patch", map[string]any{"file_path": "main.go", "success": true}, 0),
		toolActivity(start, 5, 6, "retry", "agent-1", "exec_command", map[string]any{"command": "go test ./...", "exit_code": 0}, 0),
	}

	report := AnalyzeRework(Session{ActivityCount: int64(len(activities))}, activities)

	if report.ReworkDuration != 5*time.Second || report.TotalAgentEffort != 10*time.Second {
		t.Fatalf("effort = %s of %s, want cycle-window intersection 5s of 10s", report.ReworkDuration, report.TotalAgentEffort)
	}
	if report.ReworkAgentEffortRate == nil || *report.ReworkAgentEffortRate != 0.5 {
		t.Fatalf("rework effort rate = %#v, want 0.5", report.ReworkAgentEffortRate)
	}
}

func TestAnalyzeReworkDoesNotReportEffortRateWithoutObservedDuration(t *testing.T) {
	report := AnalyzeRework(Session{ActivityCount: 1}, []Activity{{AgentID: "main"}})

	if report.TotalAgentEffort != 0 || report.ReworkAgentEffortRate != nil {
		t.Fatalf("unexpected effort without duration evidence: %#v", report)
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
		Signal: canonical.SignalTrace, Kind: canonical.ActivityTool, ToolName: toolName, Name: toolName, AgentID: agentID,
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
