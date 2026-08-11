package query

import (
	"fmt"
	"sort"
	"time"
)

const AnalysisRuleVersion = "v1"

const (
	ActivityCoverageComplete = "observed_projection_complete"
	ActivityCoveragePartial  = "partial_page"
)

type RunEfficiency struct {
	WallDuration   time.Duration
	ActiveDuration time.Duration
	Parallelism    float64
	Complete       bool
}

type Evidence struct {
	Source   string `json:"source"`
	RunID    string `json:"runId"`
	TraceID  string `json:"traceId,omitempty"`
	SpanID   string `json:"spanId,omitempty"`
	Name     string `json:"name"`
	AgentID  string `json:"agentId,omitempty"`
	Activity string `json:"activity"`
}

type Finding struct {
	ID           string
	Kind         string
	Severity     string
	Summary      string
	Metric       string
	Value        float64
	Unit         string
	Confidence   string
	Completeness string
	Evidence     []Evidence
}

func AnalyzeRun(summary Session, activities []Activity) RunEfficiency {
	wall := positiveDuration(summary.StartedAt, summary.EndedAt)
	intervalsByAgent := make(map[string][]timeInterval)
	for _, activity := range activities {
		if duration := positiveDuration(activity.StartedAt, activity.EndedAt); duration > 0 {
			agentID := activity.AgentID
			if agentID == "" {
				agentID = "main"
			}
			intervalsByAgent[agentID] = append(intervalsByAgent[agentID], timeInterval{start: activity.StartedAt, end: activity.EndedAt})
		}
	}
	var active time.Duration
	for _, intervals := range intervalsByAgent {
		active += mergedDuration(intervals)
	}
	parallelism := 0.0
	if wall > 0 {
		parallelism = float64(active) / float64(wall)
	}
	return RunEfficiency{
		WallDuration: wall, ActiveDuration: active, Parallelism: parallelism,
		Complete: ActivityCoverage(summary, activities) == ActivityCoverageComplete,
	}
}

func ActivityCoverage(summary Session, activities []Activity) string {
	if int64(len(activities)) >= summary.ActivityCount {
		return ActivityCoverageComplete
	}
	return ActivityCoveragePartial
}

type timeInterval struct {
	start time.Time
	end   time.Time
}

func mergedDuration(intervals []timeInterval) time.Duration {
	if len(intervals) == 0 {
		return 0
	}
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].start.Before(intervals[j].start) })
	start, end := intervals[0].start, intervals[0].end
	var total time.Duration
	for _, interval := range intervals[1:] {
		if !interval.start.After(end) {
			if interval.end.After(end) {
				end = interval.end
			}
			continue
		}
		total += end.Sub(start)
		start, end = interval.start, interval.end
	}
	return total + end.Sub(start)
}

func FindBottlenecks(summary Session, activities []Activity) []Finding {
	findings := make([]Finding, 0)
	for _, activity := range activities {
		duration := positiveDuration(activity.StartedAt, activity.EndedAt)
		if duration <= 0 {
			continue
		}
		findings = append(findings, Finding{
			ID: "activity-duration/" + evidenceKey(activity), Kind: "activity_duration",
			Severity: "info", Summary: fmt.Sprintf("Activity %q consumed notable observed wall time", activity.Name),
			Metric: "duration_ms", Value: float64(duration.Milliseconds()), Unit: "milliseconds",
			Confidence: "observed", Completeness: ActivityCoverage(summary, activities), Evidence: []Evidence{activityEvidence(activity)},
		})
	}
	sort.SliceStable(findings, func(i, j int) bool { return findings[i].Value > findings[j].Value })
	if len(findings) > 5 {
		findings = findings[:5]
	}
	return findings
}

func FindCoordinationRisks(summary Session, activities []Activity) []Finding {
	findings := make([]Finding, 0)
	for _, activity := range activities {
		if activity.Status == "error" {
			findings = append(findings, Finding{
				ID: "coordination/error/" + evidenceKey(activity), Kind: "agent_error", Severity: "high",
				Summary: fmt.Sprintf("Agent activity %q ended with an error", activity.Name),
				Metric:  "error_activity", Value: 1, Unit: "activity", Confidence: "observed",
				Completeness: ActivityCoverage(summary, activities), Evidence: []Evidence{activityEvidence(activity)},
			})
		}
		if activity.Kind == "delegation" && activity.TargetAgentID == "" && activity.TargetAgentType == "" {
			findings = append(findings, Finding{
				ID: "coordination/unknown-target/" + evidenceKey(activity), Kind: "unknown_delegation_target", Severity: "medium",
				Summary: fmt.Sprintf("Delegation %q has no reported target agent", activity.Name),
				Metric:  "delegation_target_missing", Value: 1, Unit: "activity", Confidence: "observed",
				Completeness: ActivityCoverage(summary, activities), Evidence: []Evidence{activityEvidence(activity)},
			})
		}
	}
	return findings
}

func positiveDuration(start, end time.Time) time.Duration {
	if start.IsZero() || end.Before(start) {
		return 0
	}
	return end.Sub(start)
}

func activityEvidence(activity Activity) Evidence {
	return Evidence{Source: activity.Source, RunID: activity.RunID, TraceID: activity.TraceID, SpanID: activity.SpanID, Name: activity.Name, AgentID: activity.AgentID, Activity: string(activity.Kind)}
}

func evidenceKey(activity Activity) string {
	if activity.SpanID != "" {
		return activity.TraceID + "/" + activity.SpanID
	}
	return activity.Source + "/" + activity.RunID + "/" + activity.Name + "/" + activity.ObservedAt.UTC().Format(time.RFC3339Nano)
}
