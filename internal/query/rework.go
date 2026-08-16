package query

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/theoden9014/agentmetry/internal/canonical"
)

const (
	CapabilityUnavailable = "unavailable"
)

type AnalysisCapability struct {
	State  string `json:"state"`
	Reason string `json:"reason"`
}

type ReworkCapabilities struct {
	ChangeRevert      AnalysisCapability `json:"changeRevertDetection"`
	CrossAgentOverlap AnalysisCapability `json:"crossAgentWorkOverlap"`
}

type ReworkCoverage struct {
	ActivityCoverage                   string `json:"activityCoverage"`
	CanonicalEvents                    int64  `json:"canonicalEvents"`
	ClassifiedEvents                   int64  `json:"classifiedEvents"`
	KnownOutcomes                      int64  `json:"knownOutcomes"`
	ValidationAttempts                 int64  `json:"validationAttempts"`
	FingerprintedFailures              int64  `json:"fingerprintedFailures"`
	IdentifiedValidationAttempts       int64  `json:"identifiedValidationAttempts"`
	IDBackedValidationAttempts         int64  `json:"idBackedValidationAttempts"`
	MergedValidationAttempts           int64  `json:"mergedValidationAttempts"`
	UncorrelatedValidationObservations int64  `json:"uncorrelatedValidationObservations"`
	ConflictingAttemptObservations     int64  `json:"conflictingAttemptObservations"`
	AmbiguousFailureAttempts           int64  `json:"ambiguousFailureAttempts"`
}

type APIRetryWaste struct {
	Attempts int64                `json:"attempts"`
	Duration time.Duration        `json:"duration"`
	Tokens   canonical.TokenUsage `json:"tokens"`
}

type ReworkCycle struct {
	Operation canonical.Operation   `json:"operation"`
	Target    canonical.EventTarget `json:"target"`
	Evidence  []Evidence            `json:"evidence"`
	agentID   string
	eventKeys []string
	startedAt time.Time
	endedAt   time.Time
}

type RecurringFailureEpisode struct {
	AgentID               string               `json:"agentId"`
	Operation             canonical.Operation  `json:"operation"`
	ValidationFingerprint string               `json:"validationFingerprint"`
	ErrorFingerprints     []string             `json:"errorFingerprints"`
	FailureAttempts       int64                `json:"failureAttempts"`
	Resolved              bool                 `json:"resolved"`
	ResolutionDuration    time.Duration        `json:"resolutionDuration"`
	ResolutionTokens      canonical.TokenUsage `json:"resolutionTokens"`
	TraceID               string               `json:"traceId,omitempty"`
	SpanID                string               `json:"spanId,omitempty"`
}

type ReworkReport struct {
	ValidationFailures            int64                     `json:"validationFailures"`
	FailFixRetryCycles            int64                     `json:"failFixRetryCycles"`
	ReworkDuration                time.Duration             `json:"reworkDuration"`
	TotalAgentEffort              time.Duration             `json:"totalAgentEffort"`
	ReworkAgentEffortRate         *float64                  `json:"reworkAgentEffortRate"`
	ReworkTokens                  canonical.TokenUsage      `json:"reworkTokens"`
	ToolAttemptsWithOutcome       int64                     `json:"toolAttemptsWithOutcome"`
	ToolFailures                  int64                     `json:"toolFailures"`
	ToolFailureRate               *float64                  `json:"toolFailureRate"`
	APIRetryWaste                 APIRetryWaste             `json:"apiRetryWaste"`
	RepeatedCommands              int64                     `json:"repeatedCommands"`
	ReeditedFiles                 int64                     `json:"reeditedFiles"`
	ValidationAttemptsWithOutcome int64                     `json:"validationAttemptsWithOutcome"`
	FirstPassEligibleValidations  int64                     `json:"firstPassEligibleValidations"`
	FirstPassSuccesses            int64                     `json:"firstPassSuccesses"`
	FirstPassSuccessRate          *float64                  `json:"firstPassSuccessRate"`
	RecurringFailureLoops         int64                     `json:"recurringFailureLoops"`
	RepeatedFailureAttempts       int64                     `json:"repeatedFailureAttempts"`
	ResolvedFailureLoops          int64                     `json:"resolvedFailureLoops"`
	UnresolvedFailureLoops        int64                     `json:"unresolvedFailureLoops"`
	FailureResolutionDuration     time.Duration             `json:"failureResolutionDuration"`
	FailureResolutionTokens       canonical.TokenUsage      `json:"failureResolutionTokens"`
	FailureEpisodes               []RecurringFailureEpisode `json:"failureEpisodes"`
	Cycles                        []ReworkCycle             `json:"cycles"`
	Coverage                      ReworkCoverage            `json:"coverage"`
	Capabilities                  ReworkCapabilities        `json:"capabilities"`
}

type SessionRework struct {
	SourceID string       `json:"sourceId"`
	RunID    string       `json:"runId"`
	Report   ReworkReport `json:"report"`
}

type SessionReworkReader interface {
	GetSessionRework(context.Context, ConversationIdentity) (SessionRework, error)
}

// CanonicalizeActivity projects one stored activity into the stable vocabulary
// used by rework analysis. It is deliberately total: malformed optional
// provider payloads reduce coverage instead of failing the entire session.
func CanonicalizeActivity(activity Activity) canonical.Event {
	attributes := activity.Attributes
	command := normalizeCommand(firstNestedString(attributes, "command", "cmd", "full_command"))
	file := firstNestedString(attributes, "file_path", "filepath", "path")
	if file == "" {
		file = patchTarget(attributes, activity.Content)
	}
	operation := classifyOperation(activity.Name, activity.ToolName, command, attributes)
	if operation == canonical.OperationAPICall && command == "" {
		command = firstNestedString(attributes, "gen_ai.request.model", "model")
		if command == "" {
			command = activity.Model
		}
	}
	return canonical.Event{
		Source: activity.Source, RunID: activity.RunID, AgentID: activity.AgentID,
		ParentAgentID: activity.ParentAgentID, Operation: operation,
		Target:  canonical.EventTarget{File: strings.TrimSpace(file), Command: command, WorkingDirectory: firstNestedString(attributes, "cwd", "working_directory", "workdir")},
		Success: observedSuccess(activity), StartedAt: activity.StartedAt,
		EndedAt: activity.EndedAt, ObservedAt: activity.ObservedAt,
		Duration: positiveDuration(activity.StartedAt, activity.EndedAt),
		Tokens:   activity.Tokens, ContributesToTotal: activity.ContributesToTotal,
		TraceID: activity.TraceID, SpanID: activity.SpanID, Name: activity.Name,
		ToolName: activity.ToolName, Tool: activity.Kind == canonical.ActivityTool || activity.ToolName != "",
		AttemptID:        canonicalAttemptID(activity),
		Signal:           activity.Signal,
		ErrorFingerprint: failureFingerprint(activity),
	}
}

func AnalyzeRework(summary Session, activities []Activity) ReworkReport {
	events := make([]canonical.Event, 0, len(activities))
	for _, activity := range activities {
		events = append(events, CanonicalizeActivity(activity))
	}
	sort.SliceStable(events, func(i, j int) bool {
		first, second := eventTime(events[i]), eventTime(events[j])
		if first.Equal(second) {
			return canonicalEvidenceKey(events[i]) < canonicalEvidenceKey(events[j])
		}
		return first.Before(second)
	})

	report := ReworkReport{
		Coverage: ReworkCoverage{ActivityCoverage: ActivityCoverage(summary, activities), CanonicalEvents: int64(len(events))},
		Capabilities: ReworkCapabilities{
			ChangeRevert:      AnalysisCapability{State: CapabilityUnavailable, Reason: "requires before/after file content or VCS diff identity"},
			CrossAgentOverlap: AnalysisCapability{State: CapabilityUnavailable, Reason: "requires stable artifact/change identity across agents"},
		},
	}
	commandSeen := make(map[string]struct{})
	fileSeen := make(map[string]struct{})
	for _, event := range events {
		if event.Operation != canonical.OperationOther {
			report.Coverage.ClassifiedEvents++
		}
		if event.Success != nil {
			report.Coverage.KnownOutcomes++
		}
		if event.Tool && event.Success != nil {
			report.ToolAttemptsWithOutcome++
			if !*event.Success {
				report.ToolFailures++
			}
		}
		if repeatableCommandOperation(event.Operation) && event.Target.Command != "" {
			key := event.AgentID + "\x00" + event.Target.Command
			if _, exists := commandSeen[key]; exists {
				report.RepeatedCommands++
			} else {
				commandSeen[key] = struct{}{}
			}
		}
		if event.Operation == canonical.OperationEdit && event.Target.File != "" {
			key := event.AgentID + "\x00" + event.Target.File
			if _, exists := fileSeen[key]; exists {
				report.ReeditedFiles++
			} else {
				fileSeen[key] = struct{}{}
			}
		}
	}
	if report.ToolAttemptsWithOutcome > 0 {
		rate := float64(report.ToolFailures) / float64(report.ToolAttemptsWithOutcome)
		report.ToolFailureRate = &rate
	}
	validationProjection := projectValidationAttempts(events)
	validationAttempts := validationProjection.attempts
	report.Coverage.ValidationAttempts = int64(len(validationAttempts))
	report.Coverage.IDBackedValidationAttempts = validationProjection.idBackedAttempts
	report.Coverage.MergedValidationAttempts = validationProjection.mergedAttempts
	report.Coverage.UncorrelatedValidationObservations = validationProjection.uncorrelatedObservations
	report.Coverage.ConflictingAttemptObservations = validationProjection.conflictingObservations
	report.Coverage.AmbiguousFailureAttempts = validationProjection.ambiguousFailures
	firstValidationOutcome := make(map[string]bool)
	for _, attempt := range validationAttempts {
		if validationIdentity(attempt) != "" {
			report.Coverage.IdentifiedValidationAttempts++
		}
		if explicitlyFailed(attempt) {
			report.ValidationFailures++
			if attempt.ErrorFingerprint != "" {
				report.Coverage.FingerprintedFailures++
			}
		}
		if attempt.Success == nil {
			continue
		}
		report.ValidationAttemptsWithOutcome++
		if attempt.AttemptID == "" {
			continue
		}
		key := validationIdentity(attempt)
		if key == "" {
			continue
		}
		if _, observed := firstValidationOutcome[key]; !observed {
			firstValidationOutcome[key] = *attempt.Success
			if *attempt.Success {
				report.FirstPassSuccesses++
			}
		}
	}
	if len(firstValidationOutcome) > 0 {
		report.FirstPassEligibleValidations = int64(len(firstValidationOutcome))
		rate := float64(report.FirstPassSuccesses) / float64(len(firstValidationOutcome))
		report.FirstPassSuccessRate = &rate
	}

	episodes := detectFailureEpisodes(validationAttempts)
	for _, episode := range episodes {
		if !episode.recurring() {
			continue
		}
		report.RecurringFailureLoops++
		report.RepeatedFailureAttempts += episode.recurringAttempts()
		if episode.resolved {
			report.ResolvedFailureLoops++
			report.FailureResolutionDuration += positiveDuration(episode.startedAt, episode.endedAt)
		} else {
			report.UnresolvedFailureLoops++
		}
		report.FailureEpisodes = append(report.FailureEpisodes, mapRecurringFailureEpisode(events, episode))
	}
	report.FailureResolutionTokens = aggregateFailureResolutionTokens(events, episodes)

	report.Cycles = detectReworkCycles(events)
	report.FailFixRetryCycles = int64(len(report.Cycles))
	report.ReworkDuration, report.ReworkTokens = aggregateCycleEffort(events, report.Cycles)
	report.TotalAgentEffort = AnalyzeRun(summary, activities).ActiveDuration
	if report.TotalAgentEffort > 0 {
		rate := float64(report.ReworkDuration) / float64(report.TotalAgentEffort)
		report.ReworkAgentEffortRate = &rate
	}
	report.APIRetryWaste = detectAPIRetryWaste(events)
	return report
}

type failureEpisode struct {
	agentID         string
	startedAt       time.Time
	endedAt         time.Time
	resolved        bool
	failureAttempts int64
	fingerprints    map[string]int64
	operation       canonical.Operation
	target          canonical.EventTarget
	traceID         string
	spanID          string
}

func (episode failureEpisode) recurring() bool {
	for _, attempts := range episode.fingerprints {
		if attempts >= 2 {
			return true
		}
	}
	return false
}

func (episode failureEpisode) recurringAttempts() int64 {
	return episode.failureAttempts
}

func detectFailureEpisodes(events []canonical.Event) []failureEpisode {
	pending := make(map[string]*failureEpisode)
	completed := make([]failureEpisode, 0)
	for _, event := range events {
		if !isValidationOperation(event.Operation) || event.Success == nil || event.AttemptID == "" {
			continue
		}
		key := validationIdentity(event)
		if key == "" {
			continue
		}
		if *event.Success {
			if episode, exists := pending[key]; exists {
				episode.endedAt = eventEnd(event)
				episode.resolved = true
				completed = append(completed, *episode)
				delete(pending, key)
			}
			continue
		}
		episode, exists := pending[key]
		if !exists {
			episode = &failureEpisode{agentID: event.AgentID, startedAt: eventTime(event), endedAt: eventEnd(event), fingerprints: make(map[string]int64), operation: event.Operation, target: event.Target, traceID: event.TraceID, spanID: event.SpanID}
			pending[key] = episode
		}
		episode.endedAt = eventEnd(event)
		episode.failureAttempts++
		if event.ErrorFingerprint != "" {
			episode.fingerprints[event.ErrorFingerprint]++
		}
	}
	for _, episode := range pending {
		completed = append(completed, *episode)
	}
	sort.SliceStable(completed, func(i, j int) bool { return completed[i].startedAt.Before(completed[j].startedAt) })
	return completed
}

func aggregateFailureResolutionTokens(events []canonical.Event, episodes []failureEpisode) canonical.TokenUsage {
	counted := make(map[string]struct{})
	var usage canonical.TokenUsage
	for _, episode := range episodes {
		if !episode.resolved || !episode.recurring() {
			continue
		}
		usage.Add(tokensInFailureEpisode(events, episode, counted))
	}
	return usage
}

func mapRecurringFailureEpisode(events []canonical.Event, episode failureEpisode) RecurringFailureEpisode {
	fingerprints := make([]string, 0)
	for fingerprint, attempts := range episode.fingerprints {
		if attempts >= 2 {
			fingerprints = append(fingerprints, fingerprint)
		}
	}
	sort.Strings(fingerprints)
	result := RecurringFailureEpisode{
		AgentID: episode.agentID, Operation: episode.operation, ValidationFingerprint: commandFingerprint(episode.target),
		ErrorFingerprints: fingerprints, FailureAttempts: episode.failureAttempts, Resolved: episode.resolved,
		TraceID: episode.traceID, SpanID: episode.spanID,
	}
	if episode.resolved {
		result.ResolutionDuration = positiveDuration(episode.startedAt, episode.endedAt)
		result.ResolutionTokens = tokensInFailureEpisode(events, episode, nil)
	}
	return result
}

func commandFingerprint(target canonical.EventTarget) string {
	digest := sha256.Sum256([]byte(target.WorkingDirectory + "\x00" + target.Command))
	return fmt.Sprintf("sha256:%x", digest[:12])
}

func tokensInFailureEpisode(events []canonical.Event, episode failureEpisode, counted map[string]struct{}) canonical.TokenUsage {
	var usage canonical.TokenUsage
	for _, event := range events {
		if effortAgentID(event.AgentID) != effortAgentID(episode.agentID) || !event.ContributesToTotal {
			continue
		}
		observedAt := event.ObservedAt
		if observedAt.IsZero() {
			observedAt = eventEnd(event)
		}
		if observedAt.Before(episode.startedAt) || observedAt.After(episode.endedAt) {
			continue
		}
		key := canonicalEvidenceKey(event)
		if counted != nil {
			if _, exists := counted[key]; exists {
				continue
			}
			counted[key] = struct{}{}
		}
		usage.Add(event.Tokens)
	}
	return usage
}

type validationAttemptProjection struct {
	attempts                 []canonical.Event
	idBackedAttempts         int64
	mergedAttempts           int64
	uncorrelatedObservations int64
	conflictingObservations  int64
	ambiguousFailures        int64
}

func projectValidationAttempts(events []canonical.Event) validationAttemptProjection {
	observations := make([]canonical.Event, 0)
	indexes := make(map[string]int)
	observationCounts := make(map[string]int64)
	projection := validationAttemptProjection{}
	for _, event := range events {
		identity := event.AttemptID
		if identity == "" {
			if isValidationOperation(event.Operation) {
				observations = append(observations, event)
				projection.uncorrelatedObservations++
			}
			continue
		}
		if index, exists := indexes[identity]; exists {
			merged, conflict := mergeValidationObservations(observations[index], event)
			if conflict {
				projection.conflictingObservations++
				if isValidationOperation(event.Operation) {
					event.AttemptID = ""
					observations = append(observations, event)
					projection.uncorrelatedObservations++
				}
				continue
			}
			observations[index] = merged
			observationCounts[identity]++
			continue
		}
		indexes[identity] = len(observations)
		observations = append(observations, event)
		observationCounts[identity] = 1
	}
	projection.attempts = make([]canonical.Event, 0, len(observations))
	for _, observation := range observations {
		if isValidationOperation(observation.Operation) {
			projection.attempts = append(projection.attempts, observation)
			if observation.FailureEvidenceAmbiguous {
				projection.ambiguousFailures++
			}
			if observation.AttemptID != "" {
				projection.idBackedAttempts++
				if observationCounts[observation.AttemptID] > 1 {
					projection.mergedAttempts++
				}
			}
		}
	}
	return projection
}

func mergeValidationObservations(current, observation canonical.Event) (canonical.Event, bool) {
	if isValidationOperation(current.Operation) && isValidationOperation(observation.Operation) && current.Operation != observation.Operation {
		return current, true
	}
	if current.Target.Command != "" && observation.Target.Command != "" && current.Target.Command != observation.Target.Command {
		return current, true
	}
	if current.AgentID != "" && observation.AgentID != "" && current.AgentID != observation.AgentID {
		return current, true
	}
	if !isValidationOperation(current.Operation) && isValidationOperation(observation.Operation) {
		current.Operation = observation.Operation
	}
	if current.Target.Command == "" {
		current.Target.Command = observation.Target.Command
	}
	if current.AgentID == "" {
		current.AgentID = observation.AgentID
	}
	if current.Target.WorkingDirectory == "" {
		current.Target.WorkingDirectory = observation.Target.WorkingDirectory
	}
	if current.FailureEvidenceAmbiguous {
		// Once conflicting evidence is observed, later ordering cannot restore a
		// single trustworthy fingerprint for this logical attempt.
	} else if current.ErrorFingerprint == "" {
		current.ErrorFingerprint = observation.ErrorFingerprint
	} else if observation.ErrorFingerprint != "" && current.ErrorFingerprint != observation.ErrorFingerprint {
		current.ErrorFingerprint = ""
		current.FailureEvidenceAmbiguous = true
	}
	if current.Success == nil || observation.Success != nil && !*observation.Success {
		current.Success = observation.Success
	}
	if current.StartedAt.IsZero() || !observation.StartedAt.IsZero() && observation.StartedAt.Before(current.StartedAt) {
		current.StartedAt = observation.StartedAt
	}
	if observation.EndedAt.After(current.EndedAt) {
		current.EndedAt = observation.EndedAt
	}
	if observation.ObservedAt.After(current.ObservedAt) {
		current.ObservedAt = observation.ObservedAt
	}
	return current, false
}

func validationIdentity(event canonical.Event) string {
	if !isValidationOperation(event.Operation) || event.Target.Command == "" {
		return ""
	}
	return effortAgentID(event.AgentID) + "\x00" + string(event.Operation) + "\x00" + event.Target.WorkingDirectory + "\x00" + event.Target.Command
}

type retryCandidate struct {
	index     int
	corrected bool
}

func detectReworkCycles(events []canonical.Event) []ReworkCycle {
	pending := make(map[string]retryCandidate)
	cycles := make([]ReworkCycle, 0)
	for index, event := range events {
		if event.Operation == canonical.OperationEdit {
			for key, candidate := range pending {
				if strings.HasPrefix(key, event.AgentID+"\x00") {
					candidate.corrected = true
					pending[key] = candidate
				}
			}
		}
		if !retryableOperation(event.Operation) {
			continue
		}
		key := retryKey(event)
		if key == "" {
			continue
		}
		if candidate, exists := pending[key]; exists && candidate.corrected {
			evidence := make([]Evidence, 0, index-candidate.index+1)
			eventKeys := make([]string, 0, index-candidate.index+1)
			for _, item := range events[candidate.index : index+1] {
				if item.AgentID != event.AgentID {
					continue
				}
				evidence = append(evidence, canonicalEvidence(item))
				eventKeys = append(eventKeys, canonicalEvidenceKey(item))
			}
			cycles = append(cycles, ReworkCycle{
				Operation: event.Operation, Target: event.Target, Evidence: evidence, eventKeys: eventKeys,
				agentID: event.AgentID, startedAt: eventTime(events[candidate.index]), endedAt: eventEnd(event),
			})
			delete(pending, key)
		}
		if explicitlyFailed(event) {
			pending[key] = retryCandidate{index: index}
		}
	}
	return cycles
}

func aggregateCycleEffort(events []canonical.Event, cycles []ReworkCycle) (time.Duration, canonical.TokenUsage) {
	eventsByKey := make(map[string]canonical.Event, len(events))
	for _, event := range events {
		eventsByKey[canonicalEvidenceKey(event)] = event
	}
	intervalsByAgent := make(map[string][]timeInterval)
	countedTokens := make(map[string]struct{})
	var tokens canonical.TokenUsage
	for _, cycle := range cycles {
		agentID := effortAgentID(cycle.agentID)
		for _, event := range events {
			if event.Duration <= 0 || effortAgentID(event.AgentID) != agentID {
				continue
			}
			start, end := event.StartedAt, event.EndedAt
			if start.Before(cycle.startedAt) {
				start = cycle.startedAt
			}
			if end.After(cycle.endedAt) {
				end = cycle.endedAt
			}
			if end.After(start) {
				intervalsByAgent[agentID] = append(intervalsByAgent[agentID], timeInterval{start: start, end: end})
			}
		}
		for _, key := range cycle.eventKeys {
			event, exists := eventsByKey[key]
			if !exists {
				continue
			}
			if _, counted := countedTokens[key]; !counted && event.ContributesToTotal {
				tokens.Add(event.Tokens)
				countedTokens[key] = struct{}{}
			}
		}
	}
	var duration time.Duration
	for _, intervals := range intervalsByAgent {
		duration += mergedDuration(intervals)
	}
	return duration, tokens
}

func effortAgentID(agentID string) string {
	if agentID == "" {
		return "main"
	}
	return agentID
}

func detectAPIRetryWaste(events []canonical.Event) APIRetryWaste {
	var waste APIRetryWaste
	for index, event := range events {
		if event.Operation != canonical.OperationAPICall || !explicitlyFailed(event) {
			continue
		}
		key := retryKey(event)
		if key == "" {
			continue
		}
		retried := false
		for _, later := range events[index+1:] {
			if later.Operation == canonical.OperationAPICall && retryKey(later) == key {
				retried = true
				break
			}
		}
		if !retried {
			continue
		}
		waste.Attempts++
		waste.Duration += event.Duration
		if event.ContributesToTotal {
			waste.Tokens.Add(event.Tokens)
		}
	}
	return waste
}

func classifyOperation(name, toolName, command string, attributes map[string]any) canonical.Operation {
	combined := strings.ToLower(strings.Join([]string{name, toolName, firstNestedString(attributes, "event.name")}, " "))
	tool := strings.ToLower(strings.TrimSpace(toolName))
	commandLower := strings.ToLower(command)
	switch {
	case isTestCommand(commandLower) || containsWord(combined, "test"):
		return canonical.OperationTest
	case isBuildCommand(commandLower) || containsWord(combined, "build"):
		return canonical.OperationBuild
	case isLintCommand(commandLower) || strings.Contains(combined, "lint"):
		return canonical.OperationLint
	case containsAny(combined, "model.request", "model.error", "api_request", "api_error", "api.retry"):
		return canonical.OperationAPICall
	case tool == "apply_patch" || tool == "write_file" || tool == "write" || tool == "edit" || tool == "notebook_update":
		return canonical.OperationEdit
	case tool == "read_file" || tool == "view_image" || tool == "read" || tool == "grep" || tool == "glob" || tool == "search" || tool == "find":
		return canonical.OperationRead
	case command != "" || containsAny(combined, "exec_command", "write_stdin", "execute", "shell", "bash", "terminal"):
		return canonical.OperationExecute
	default:
		return canonical.OperationOther
	}
}

func observedSuccess(activity Activity) *bool {
	switch strings.ToLower(strings.TrimSpace(activity.Status)) {
	case "ok", "success", "succeeded":
		return boolValue(true)
	case "error", "failed", "failure":
		return boolValue(false)
	}
	if value, ok := firstNestedValue(activity.Attributes, "success", "tool.success"); ok {
		if success, ok := parseBool(value); ok {
			return boolValue(success)
		}
	}
	if value, ok := firstNestedValue(activity.Attributes, "exit_code", "exitCode", "status_code"); ok {
		if code, ok := parseInt64(value); ok {
			return boolValue(code == 0)
		}
	}
	if value, ok := firstNestedValue(activity.Attributes, "error"); ok && nonEmptyValue(value) {
		return boolValue(false)
	}
	combined := strings.ToLower(activity.Name + " " + firstNestedString(activity.Attributes, "event.name"))
	if containsAny(combined, ".error", "_error", " failure", " failed") {
		return boolValue(false)
	}
	return nil
}

func canonicalAttemptID(activity Activity) string {
	namespace := strings.Join([]string{activity.Source, activity.RunID, effortAgentID(activity.AgentID)}, "/")
	if explicit := firstNestedString(activity.Attributes, "gen_ai.tool.call.id", "tool_use_id", "tool_call_id", "call_id"); explicit != "" {
		return "tool-call/" + namespace + "/" + explicit
	}
	if activity.Signal == canonical.SignalTrace && activity.SpanID != "" && (activity.Kind == canonical.ActivityTool || activity.ToolName != "") {
		return "tool-span/" + namespace + "/" + activity.TraceID + "/" + activity.SpanID
	}
	return ""
}

var (
	ansiEscapePattern    = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
	isoTimestampPattern  = regexp.MustCompile(`(?i)\b\d{4}-\d{2}-\d{2}[t ][0-9:.+-]+z?\b`)
	uuidPattern          = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	locationPattern      = regexp.MustCompile(`(?i)(?:[a-z]:)?(?:[/\\][^/\\\s:]+)*[/\\]?[a-z0-9_.-]+\.[a-z0-9]+:\d+(?::\d+)?`)
	absolutePathPattern  = regexp.MustCompile(`(?i)(?:[a-z]:)?[/\\](?:[^/\\\s:]+[/\\])*[^/\\\s:]+`)
	durationPattern      = regexp.MustCompile(`\b\d+(?:\.\d+)?(?:ns|us|µs|ms|s|min)\b`)
	hexIdentifierPattern = regexp.MustCompile(`(?i)\b0x[0-9a-f]+\b`)
	lineSuffixPattern    = regexp.MustCompile(`:\d+(?::\d+)?$`)
)

func failureFingerprint(activity Activity) string {
	success := observedSuccess(activity)
	if success == nil || *success {
		return ""
	}
	evidence := firstNestedString(activity.Attributes, "stderr", "error", "error_message", "exception.message", "message")
	if evidence == "" {
		evidence = plainFailureOutput(activity.Attributes, "output", "result")
	}
	normalized := normalizeFailureEvidence(evidence)
	if normalized == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("sha256:%x", digest[:12])
}

func plainFailureOutput(attributes map[string]any, keys ...string) string {
	for _, key := range keys {
		value, exists := attributes[key]
		if !exists {
			continue
		}
		text, ok := value.(string)
		if !ok {
			continue
		}
		text = strings.TrimSpace(text)
		if text != "" && decodeObject(text) == nil {
			return text
		}
	}
	return ""
}

func normalizeFailureEvidence(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = ansiEscapePattern.ReplaceAllString(value, "")
	value = isoTimestampPattern.ReplaceAllString(value, "<timestamp>")
	value = uuidPattern.ReplaceAllString(value, "<uuid>")
	value = locationPattern.ReplaceAllStringFunc(value, normalizeFailureLocation)
	value = absolutePathPattern.ReplaceAllStringFunc(value, normalizeFailurePath)
	value = durationPattern.ReplaceAllString(value, "<duration>")
	value = hexIdentifierPattern.ReplaceAllString(value, "<hex>")
	return strings.Join(strings.Fields(value), " ")
}

func normalizeFailureLocation(value string) string {
	withoutLine := lineSuffixPattern.ReplaceAllString(value, "")
	return "<file:" + trailingPath(withoutLine, 2) + ">:<line>"
}

func normalizeFailurePath(value string) string {
	return "<path>/" + trailingPath(value, 2)
}

func trailingPath(value string, segments int) string {
	parts := strings.FieldsFunc(strings.TrimSpace(value), func(r rune) bool { return r == '/' || r == '\\' })
	if len(parts) > segments {
		parts = parts[len(parts)-segments:]
	}
	return strings.Join(parts, "/")
}

func firstNestedString(attributes map[string]any, keys ...string) string {
	value, ok := firstNestedValue(attributes, keys...)
	if !ok {
		return ""
	}
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func firstNestedValue(attributes map[string]any, keys ...string) (any, bool) {
	objects := []map[string]any{attributes}
	for _, container := range []string{"arguments", "input", "tool_input", "tool_parameters", "output", "result"} {
		if value, exists := attributes[container]; exists {
			if object := decodeObject(value); object != nil {
				objects = append(objects, object)
			}
		}
	}
	for _, object := range objects {
		for _, key := range keys {
			if value, exists := object[key]; exists {
				return value, true
			}
		}
	}
	return nil, false
}

func decodeObject(value any) map[string]any {
	if object, ok := value.(map[string]any); ok {
		return object
	}
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return nil
	}
	var object map[string]any
	if json.Unmarshal([]byte(text), &object) != nil {
		return nil
	}
	return object
}

func patchTarget(attributes map[string]any, content string) string {
	patch := firstNestedString(attributes, "patch")
	if patch == "" {
		patch = content
	}
	for _, line := range strings.Split(patch, "\n") {
		for _, prefix := range []string{"*** Update File: ", "*** Add File: ", "*** Delete File: "} {
			if strings.HasPrefix(line, prefix) {
				return strings.TrimSpace(strings.TrimPrefix(line, prefix))
			}
		}
	}
	return ""
}

func normalizeCommand(command string) string { return strings.Join(strings.Fields(command), " ") }

func isTestCommand(command string) bool {
	return containsAny(command, "go test", "cargo test", "pytest", "npm test", "npm run test", "pnpm test", "yarn test", "make test", "vitest", "jest")
}

func isBuildCommand(command string) bool {
	return containsAny(command, "go build", "cargo build", "npm run build", "pnpm build", "yarn build", "make build", "docker build")
}

func isLintCommand(command string) bool {
	return containsAny(command, "go vet", "golangci-lint", "eslint", "npm run lint", "pnpm lint", "yarn lint", "make lint", "ruff check")
}

func containsWord(value, word string) bool {
	for _, field := range strings.FieldsFunc(value, func(r rune) bool { return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') }) {
		if field == word {
			return true
		}
	}
	return false
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func explicitlyFailed(event canonical.Event) bool { return event.Success != nil && !*event.Success }
func isValidationOperation(operation canonical.Operation) bool {
	return operation == canonical.OperationTest || operation == canonical.OperationBuild || operation == canonical.OperationLint
}
func repeatableCommandOperation(operation canonical.Operation) bool {
	return operation == canonical.OperationExecute || operation == canonical.OperationTest || operation == canonical.OperationBuild || operation == canonical.OperationLint
}
func retryableOperation(operation canonical.Operation) bool {
	return repeatableCommandOperation(operation)
}

func retryKey(event canonical.Event) string {
	target := event.Target.Command
	if target == "" {
		target = event.Target.File
	}
	if target == "" {
		target = event.ToolName
	}
	if target == "" && event.Operation == canonical.OperationAPICall {
		target = "model-api"
	}
	if target == "" {
		return ""
	}
	return event.AgentID + "\x00" + string(event.Operation) + "\x00" + target
}

func eventTime(event canonical.Event) time.Time {
	if !event.StartedAt.IsZero() {
		return event.StartedAt
	}
	return event.ObservedAt
}

func eventEnd(event canonical.Event) time.Time {
	if !event.EndedAt.IsZero() {
		return event.EndedAt
	}
	return event.ObservedAt
}

func canonicalEvidence(event canonical.Event) Evidence {
	return Evidence{Source: event.Source, RunID: event.RunID, TraceID: event.TraceID, SpanID: event.SpanID, Name: event.Name, AgentID: event.AgentID, Activity: string(event.Operation)}
}

func canonicalEvidenceKey(event canonical.Event) string {
	if event.SpanID != "" {
		return event.TraceID + "/" + event.SpanID
	}
	return fmt.Sprintf("%s/%s/%s/%s/%s/%s", event.Source, event.RunID, event.AgentID, event.Operation, event.Name, event.ObservedAt.UTC().Format(time.RFC3339Nano))
}

func boolValue(value bool) *bool { return &value }

func parseBool(value any) (bool, bool) {
	switch value := value.(type) {
	case bool:
		return value, true
	case string:
		parsed, err := strconv.ParseBool(value)
		return parsed, err == nil
	default:
		return false, false
	}
}

func parseInt64(value any) (int64, bool) {
	switch value := value.(type) {
	case int:
		return int64(value), true
	case int32:
		return int64(value), true
	case int64:
		return value, true
	case float64:
		parsed := int64(value)
		return parsed, float64(parsed) == value
	case json.Number:
		parsed, err := value.Int64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseInt(value, 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func nonEmptyValue(value any) bool {
	switch value := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(value) != ""
	default:
		return true
	}
}
