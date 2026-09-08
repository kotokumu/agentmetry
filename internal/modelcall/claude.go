package modelcall

import (
	"bytes"
	"sort"
	"strconv"
	"time"

	"github.com/kotokumu/agentmetry/internal/canonical"
)

// IdentityAlias is one provider-issued identity observed on an authoritative
// Claude request event. Aliases co-occurring on an event bridge their identity
// components; aliases from different sessions never do.
type IdentityAlias struct {
	Basis    IdentityBasis
	Value    string
	Sequence uint64
}

// ClaudeEvidence is the retained, storage-neutral fact set needed to resolve
// Claude billable calls independently of evidence arrival order.
type ClaudeEvidence struct {
	ActivityID          string
	SessionID           string
	TraceID             string
	Locator             []byte
	Aliases             []IdentityAlias
	Model               string
	Mode                string
	OccurredAt          time.Time
	FilterAt            time.Time
	Usage               canonical.TokenUsage
	ProviderAmount      *int64
	ProviderAmountState string
}

// ResolvedClaudeCall is a complete current-state call projection. Storage can
// replace the prior projection atomically without reimplementing identity or
// reconciliation policy.
type ResolvedClaudeCall struct {
	ID                       string
	SessionID                string
	RepresentativeActivityID string
	IdentityBasis            IdentityBasis
	Model                    string
	Mode                     string
	OccurredAt               time.Time
	FilterAt                 time.Time
	Usage                    canonical.TokenUsage
	Evidence                 []ClaudeEvidence
	Attribution              Attribution
}

// ResolveClaudeCalls builds alias connected components, selects a stable
// representative and reconciles authoritative facts as a set.
func ResolveClaudeCalls(evidence []ClaudeEvidence) []ResolvedClaudeCall {
	parent := make([]int, len(evidence))
	for index := range parent {
		parent[index] = index
	}
	var find func(int) int
	find = func(value int) int {
		if parent[value] != value {
			parent[value] = find(parent[value])
		}
		return parent[value]
	}
	join := func(left, right int) {
		left, right = find(left), find(right)
		if left != right {
			parent[right] = left
		}
	}
	type componentAlias struct {
		session  string
		basis    IdentityBasis
		value    string
		sequence uint64
	}
	owners := make(map[componentAlias]int)
	for index, item := range evidence {
		for _, alias := range item.Aliases {
			key := componentAlias{session: item.SessionID, basis: alias.Basis, value: alias.Value, sequence: alias.Sequence}
			if previous, exists := owners[key]; exists {
				join(index, previous)
			} else {
				owners[key] = index
			}
		}
	}
	components := make(map[int][]ClaudeEvidence)
	for index, item := range evidence {
		components[find(index)] = append(components[find(index)], item)
	}
	result := make([]ResolvedClaudeCall, 0, len(components))
	for _, members := range components {
		sort.Slice(members, func(i, j int) bool { return bytes.Compare(members[i].Locator, members[j].Locator) < 0 })
		basis, alias := canonicalAlias(members)
		if basis == IdentityJournalEvidenceFallback {
			alias = JournalAlias(members[0].Locator)
		}
		call := ResolvedClaudeCall{
			ID: CallID("claude", members[0].SessionID, basis, alias), SessionID: members[0].SessionID,
			RepresentativeActivityID: members[0].ActivityID, IdentityBasis: basis,
			FilterAt: members[0].FilterAt, Evidence: members,
			Attribution: reconcileClaudeAttribution(members),
		}
		call.Model, call.Mode, call.OccurredAt, call.Usage = reconcileClaudeFacts(members)
		result = append(result, call)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func aliasKey(alias IdentityAlias) string {
	if alias.Basis == IdentityClaudeEventSequence {
		return strconv.FormatUint(alias.Sequence, 10)
	}
	return alias.Value
}

func canonicalAlias(evidence []ClaudeEvidence) (IdentityBasis, Alias) {
	var candidates []IdentityAlias
	for _, item := range evidence {
		candidates = append(candidates, item.Aliases...)
	}
	if len(candidates) == 0 {
		return IdentityJournalEvidenceFallback, Alias{}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Basis != candidates[j].Basis {
			return candidates[i].Basis < candidates[j].Basis
		}
		if candidates[i].Basis == IdentityClaudeEventSequence {
			return candidates[i].Sequence < candidates[j].Sequence
		}
		return candidates[i].Value < candidates[j].Value
	})
	selected := candidates[0]
	if selected.Basis == IdentityClaudeEventSequence {
		return selected.Basis, StringAlias(strconv.FormatUint(selected.Sequence, 10))
	}
	return selected.Basis, StringAlias(selected.Value)
}

func reconcileClaudeAttribution(evidence []ClaudeEvidence) Attribution {
	if conflictingClaudeFacts(evidence) {
		return Attribution{Basis: CostUnavailable, Reason: ReasonConflictingAuthoritativeEvidence}
	}
	amounts := make(map[int64]struct{})
	invalid := false
	for _, item := range evidence {
		switch item.ProviderAmountState {
		case "valid":
			if item.ProviderAmount == nil {
				invalid = true
			} else {
				amounts[*item.ProviderAmount] = struct{}{}
			}
		case "invalid":
			invalid = true
		}
	}
	if len(amounts) > 1 {
		return Attribution{Basis: CostUnavailable, Reason: ReasonConflictingAuthoritativeEvidence}
	}
	if invalid || len(amounts) == 0 {
		return Attribution{Basis: CostUnavailable, Reason: ReasonInvalidProviderAmount}
	}
	for amount := range amounts {
		value := amount
		return Attribution{Basis: CostProviderReported, AmountMicroUSD: &value}
	}
	panic("unreachable")
}

func conflictingClaudeFacts(evidence []ClaudeEvidence) bool {
	models := make(map[string]struct{})
	modes := make(map[string]struct{})
	times := make(map[string]struct{})
	input := make(map[int64]struct{})
	output := make(map[int64]struct{})
	cacheRead := make(map[int64]struct{})
	cacheWrite := make(map[int64]struct{})
	reasoning := make(map[int64]struct{})
	for _, item := range evidence {
		if item.Model != "" {
			models[item.Model] = struct{}{}
		}
		if item.Mode != "" {
			modes[item.Mode] = struct{}{}
		}
		if !item.OccurredAt.IsZero() {
			times[item.OccurredAt.UTC().Format(time.RFC3339Nano)] = struct{}{}
		}
		addReported(input, item.Usage.Input, item.Usage.InputReported())
		addReported(output, item.Usage.Output, item.Usage.OutputReported())
		addReported(cacheRead, item.Usage.CacheRead, item.Usage.CacheReadReported())
		addReported(cacheWrite, item.Usage.CacheWrite, item.Usage.CacheWriteReported())
		addReported(reasoning, item.Usage.Reasoning, item.Usage.ReasoningReported())
	}
	return len(models) > 1 || len(modes) > 1 || len(times) > 1 || len(input) > 1 || len(output) > 1 || len(cacheRead) > 1 || len(cacheWrite) > 1 || len(reasoning) > 1
}

func reconcileClaudeFacts(evidence []ClaudeEvidence) (string, string, time.Time, canonical.TokenUsage) {
	models := make(map[string]struct{})
	modes := make(map[string]struct{})
	times := make(map[string]time.Time)
	input := make(map[int64]struct{})
	output := make(map[int64]struct{})
	cacheRead := make(map[int64]struct{})
	cacheWrite := make(map[int64]struct{})
	reasoning := make(map[int64]struct{})
	for _, item := range evidence {
		if item.Model != "" {
			models[item.Model] = struct{}{}
		}
		if item.Mode != "" {
			modes[item.Mode] = struct{}{}
		}
		if !item.OccurredAt.IsZero() {
			times[item.OccurredAt.UTC().Format(time.RFC3339Nano)] = item.OccurredAt.UTC()
		}
		addReported(input, item.Usage.Input, item.Usage.InputReported())
		addReported(output, item.Usage.Output, item.Usage.OutputReported())
		addReported(cacheRead, item.Usage.CacheRead, item.Usage.CacheReadReported())
		addReported(cacheWrite, item.Usage.CacheWrite, item.Usage.CacheWriteReported())
		addReported(reasoning, item.Usage.Reasoning, item.Usage.ReasoningReported())
	}
	var usage canonical.TokenUsage
	usage.Input, usage.Presence.Input = soleInt64(input)
	usage.Output, usage.Presence.Output = soleInt64(output)
	usage.CacheRead, usage.Presence.CacheRead = soleInt64(cacheRead)
	usage.CacheWrite, usage.Presence.CacheWrite = soleInt64(cacheWrite)
	usage.Reasoning, usage.Presence.Reasoning = soleInt64(reasoning)
	return soleString(models), soleString(modes), soleTime(times), usage
}

func soleString(values map[string]struct{}) string {
	if len(values) != 1 {
		return ""
	}
	for value := range values {
		return value
	}
	return ""
}

func soleTime(values map[string]time.Time) time.Time {
	if len(values) != 1 {
		return time.Time{}
	}
	for _, value := range values {
		return value
	}
	return time.Time{}
}

func soleInt64(values map[int64]struct{}) (int64, bool) {
	if len(values) != 1 {
		return 0, false
	}
	for value := range values {
		return value, value == 0
	}
	return 0, false
}

func addReported(values map[int64]struct{}, value int64, reported bool) {
	if reported {
		values[value] = struct{}{}
	}
}
