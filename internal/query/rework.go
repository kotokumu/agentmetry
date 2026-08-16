package query

import (
	"context"
	"encoding/json"
	"fmt"
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
	ActivityCoverage string `json:"activityCoverage"`
	CanonicalEvents  int64  `json:"canonicalEvents"`
	ClassifiedEvents int64  `json:"classifiedEvents"`
	KnownOutcomes    int64  `json:"knownOutcomes"`
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

type ReworkReport struct {
	ValidationFailures      int64                `json:"validationFailures"`
	FailFixRetryCycles      int64                `json:"failFixRetryCycles"`
	ReworkDuration          time.Duration        `json:"reworkDuration"`
	TotalAgentEffort        time.Duration        `json:"totalAgentEffort"`
	ReworkAgentEffortRate   *float64             `json:"reworkAgentEffortRate"`
	ReworkTokens            canonical.TokenUsage `json:"reworkTokens"`
	ToolAttemptsWithOutcome int64                `json:"toolAttemptsWithOutcome"`
	ToolFailures            int64                `json:"toolFailures"`
	ToolFailureRate         *float64             `json:"toolFailureRate"`
	APIRetryWaste           APIRetryWaste        `json:"apiRetryWaste"`
	RepeatedCommands        int64                `json:"repeatedCommands"`
	ReeditedFiles           int64                `json:"reeditedFiles"`
	Cycles                  []ReworkCycle        `json:"cycles"`
	Coverage                ReworkCoverage       `json:"coverage"`
	Capabilities            ReworkCapabilities   `json:"capabilities"`
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
		Target:  canonical.EventTarget{File: strings.TrimSpace(file), Command: command},
		Success: observedSuccess(activity), StartedAt: activity.StartedAt,
		EndedAt: activity.EndedAt, ObservedAt: activity.ObservedAt,
		Duration: positiveDuration(activity.StartedAt, activity.EndedAt),
		Tokens:   activity.Tokens, ContributesToTotal: activity.ContributesToTotal,
		TraceID: activity.TraceID, SpanID: activity.SpanID, Name: activity.Name,
		ToolName: activity.ToolName, Tool: activity.Kind == canonical.ActivityTool || activity.ToolName != "",
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
		if isValidationOperation(event.Operation) && explicitlyFailed(event) {
			report.ValidationFailures++
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
