package query

import (
	"testing"
	"time"
)

func TestAnalyzeRunReportsObservedParallelismAndCompleteness(t *testing.T) {
	start := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	summary := Session{StartedAt: start, EndedAt: start.Add(10 * time.Second), ActivityCount: 2}
	activities := []Activity{
		{AgentID: "main", StartedAt: start, EndedAt: start.Add(8 * time.Second)},
		{AgentID: "child", StartedAt: start.Add(2 * time.Second), EndedAt: start.Add(6 * time.Second)},
	}

	efficiency := AnalyzeRun(summary, activities)

	if efficiency.WallDuration != 10*time.Second || efficiency.ActiveDuration != 12*time.Second {
		t.Fatalf("unexpected durations: %#v", efficiency)
	}
	if efficiency.Parallelism != 1.2 || !efficiency.Complete {
		t.Fatalf("unexpected parallelism/completeness: %#v", efficiency)
	}
}

func TestAnalyzeRunMergesNestedActivitiesWithinOneAgent(t *testing.T) {
	start := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	summary := Session{StartedAt: start, EndedAt: start.Add(10 * time.Second), ActivityCount: 2}
	activities := []Activity{
		{AgentID: "main", StartedAt: start, EndedAt: start.Add(8 * time.Second)},
		{AgentID: "main", StartedAt: start.Add(2 * time.Second), EndedAt: start.Add(6 * time.Second)},
	}

	efficiency := AnalyzeRun(summary, activities)

	if efficiency.ActiveDuration != 8*time.Second || efficiency.Parallelism != 0.8 {
		t.Fatalf("nested activities were double-counted: %#v", efficiency)
	}
}

func TestFindBottlenecksSortsObservedDurationsAndKeepsEvidence(t *testing.T) {
	start := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	activities := []Activity{
		{Source: "codex", RunID: "run-1", TraceID: "trace", SpanID: "short", Name: "short", StartedAt: start, EndedAt: start.Add(time.Second)},
		{Source: "codex", RunID: "run-1", TraceID: "trace", SpanID: "long", Name: "long", StartedAt: start, EndedAt: start.Add(4 * time.Second)},
	}

	findings := FindBottlenecks(Session{ActivityCount: 2}, activities)

	if len(findings) != 2 || findings[0].Evidence[0].SpanID != "long" || findings[0].Confidence != "observed" {
		t.Fatalf("unexpected findings: %#v", findings)
	}
}

func TestFindCoordinationRisksOnlyReportsExplicitEvidence(t *testing.T) {
	activities := []Activity{
		{Source: "claude", RunID: "run-1", Name: "delegation", Kind: "delegation"},
		{Source: "claude", RunID: "run-1", Name: "failed", Status: "error"},
	}

	findings := FindCoordinationRisks(Session{ActivityCount: 2}, activities)

	if len(findings) != 2 || findings[0].Confidence != "observed" || findings[1].Confidence != "observed" {
		t.Fatalf("unexpected coordination findings: %#v", findings)
	}
}
