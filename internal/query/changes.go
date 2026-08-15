package query

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

const projectionCursorVersion = 1

var (
	ErrProjectionCursorInvalid = errors.New("projection cursor is invalid")
	ErrProjectionCursorExpired = errors.New("projection cursor is outside retained history")
	ErrProjectionGeneration    = errors.New("projection cursor belongs to another generation")
)

type ProjectionPosition struct {
	Generation string
	Sequence   int64
}

type projectionCursorPayload struct {
	Version    int    `json:"v"`
	Generation string `json:"g"`
	Sequence   int64  `json:"s"`
}

func EncodeProjectionCursor(position ProjectionPosition) string {
	payload, _ := json.Marshal(projectionCursorPayload{Version: projectionCursorVersion, Generation: position.Generation, Sequence: position.Sequence})
	return base64.RawURLEncoding.EncodeToString(payload)
}

func DecodeProjectionCursor(token string) (ProjectionPosition, error) {
	encoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return ProjectionPosition{}, ErrProjectionCursorInvalid
	}
	var payload projectionCursorPayload
	if err := json.Unmarshal(encoded, &payload); err != nil || payload.Version != projectionCursorVersion || payload.Generation == "" || payload.Sequence < 0 {
		return ProjectionPosition{}, ErrProjectionCursorInvalid
	}
	return ProjectionPosition{Generation: payload.Generation, Sequence: payload.Sequence}, nil
}

type ChangeTargetKind string

const (
	ChangeTargetOverview    ChangeTargetKind = "overview"
	ChangeTargetSource      ChangeTargetKind = "source"
	ChangeTargetSession     ChangeTargetKind = "session"
	ChangeTargetTrace       ChangeTargetKind = "trace"
	ChangeTargetPlanUsage   ChangeTargetKind = "plan_usage"
	ChangeTargetAllSources  ChangeTargetKind = "all_sources"
	ChangeTargetAllSessions ChangeTargetKind = "all_sessions"
	ChangeTargetAllTraces   ChangeTargetKind = "all_traces"
)

type ChangeTarget struct {
	Kind      ChangeTargetKind `json:"kind"`
	SourceID  string           `json:"sourceId,omitempty"`
	SessionID string           `json:"sessionId,omitempty"`
	TraceID   string           `json:"traceId,omitempty"`
}

func OverviewTarget() ChangeTarget { return ChangeTarget{Kind: ChangeTargetOverview} }
func SourceTarget(source string) ChangeTarget {
	return ChangeTarget{Kind: ChangeTargetSource, SourceID: source}
}
func SessionTarget(source, session string) ChangeTarget {
	return ChangeTarget{Kind: ChangeTargetSession, SourceID: source, SessionID: session}
}
func TraceTarget(trace string) ChangeTarget {
	return ChangeTarget{Kind: ChangeTargetTrace, TraceID: trace}
}
func PlanUsageTarget(source string) ChangeTarget {
	return ChangeTarget{Kind: ChangeTargetPlanUsage, SourceID: source}
}
func AllSourcesTarget() ChangeTarget  { return ChangeTarget{Kind: ChangeTargetAllSources} }
func AllSessionsTarget() ChangeTarget { return ChangeTarget{Kind: ChangeTargetAllSessions} }
func AllTracesTarget() ChangeTarget   { return ChangeTarget{Kind: ChangeTargetAllTraces} }

func (target ChangeTarget) AffectsSession(source, session string) bool {
	return target.Kind == ChangeTargetAllSessions || (target.Kind == ChangeTargetSession && target.SourceID == source && target.SessionID == session)
}

func (target ChangeTarget) AffectsTrace(trace string) bool {
	return target.Kind == ChangeTargetAllTraces || (target.Kind == ChangeTargetTrace && target.TraceID == trace)
}

type ChangeTargetSet struct {
	limit  int
	values map[ChangeTarget]struct{}
	coarse map[ChangeTargetKind]bool
	counts map[ChangeTargetKind]int
}

func NewChangeTargetSet(limit int) *ChangeTargetSet {
	if limit < 1 {
		limit = 1
	}
	return &ChangeTargetSet{limit: limit, values: make(map[ChangeTarget]struct{}), coarse: make(map[ChangeTargetKind]bool), counts: make(map[ChangeTargetKind]int)}
}

func (set *ChangeTargetSet) Add(target ChangeTarget) {
	if exact, coarse := exactKind(target.Kind); coarse {
		set.deleteKind(exact)
		set.addValue(target)
		set.coarse[target.Kind] = true
		return
	}
	coarse, exact := coarseKind(target.Kind)
	if exact && set.coarse[coarse] {
		return
	}
	set.addValue(target)
	if exact && set.counts[target.Kind] > set.limit {
		set.deleteKind(target.Kind)
		set.addValue(ChangeTarget{Kind: coarse})
		set.coarse[coarse] = true
	}
	if len(set.values) > set.limit {
		set.compactAll()
	}
}

func exactKind(kind ChangeTargetKind) (ChangeTargetKind, bool) {
	switch kind {
	case ChangeTargetAllSources:
		return ChangeTargetSource, true
	case ChangeTargetAllSessions:
		return ChangeTargetSession, true
	case ChangeTargetAllTraces:
		return ChangeTargetTrace, true
	default:
		return "", false
	}
}

func (set *ChangeTargetSet) Values() []ChangeTarget {
	values := make([]ChangeTarget, 0, len(set.values))
	for target := range set.values {
		values = append(values, target)
	}
	sort.Slice(values, func(i, j int) bool {
		left, right := targetOrder(values[i].Kind), targetOrder(values[j].Kind)
		if left != right {
			return left < right
		}
		if values[i].SourceID != values[j].SourceID {
			return values[i].SourceID < values[j].SourceID
		}
		if values[i].SessionID != values[j].SessionID {
			return values[i].SessionID < values[j].SessionID
		}
		return values[i].TraceID < values[j].TraceID
	})
	return values
}

func (set *ChangeTargetSet) addValue(target ChangeTarget) {
	if _, exists := set.values[target]; exists {
		return
	}
	set.values[target] = struct{}{}
	set.counts[target.Kind]++
}

func (set *ChangeTargetSet) deleteKind(kind ChangeTargetKind) {
	for target := range set.values {
		if target.Kind == kind {
			delete(set.values, target)
		}
	}
	set.counts[kind] = 0
}

func (set *ChangeTargetSet) compactAll() {
	hasSource := set.counts[ChangeTargetSource] > 0
	hasSession := set.counts[ChangeTargetSession] > 0
	hasTrace := set.counts[ChangeTargetTrace] > 0
	hasPlan := set.counts[ChangeTargetPlanUsage] > 0
	if hasSource {
		set.deleteKind(ChangeTargetSource)
		set.addValue(AllSourcesTarget())
		set.coarse[ChangeTargetAllSources] = true
	}
	if hasSession {
		set.deleteKind(ChangeTargetSession)
		set.addValue(AllSessionsTarget())
		set.coarse[ChangeTargetAllSessions] = true
	}
	if hasTrace {
		set.deleteKind(ChangeTargetTrace)
		set.addValue(AllTracesTarget())
		set.coarse[ChangeTargetAllTraces] = true
	}
	if hasPlan {
		set.deleteKind(ChangeTargetPlanUsage)
		set.addValue(OverviewTarget())
	}
}

func coarseKind(kind ChangeTargetKind) (ChangeTargetKind, bool) {
	switch kind {
	case ChangeTargetSource:
		return ChangeTargetAllSources, true
	case ChangeTargetSession:
		return ChangeTargetAllSessions, true
	case ChangeTargetTrace:
		return ChangeTargetAllTraces, true
	default:
		return "", false
	}
}

func targetOrder(kind ChangeTargetKind) int {
	order := map[ChangeTargetKind]int{ChangeTargetOverview: 0, ChangeTargetAllSources: 1, ChangeTargetSource: 2, ChangeTargetAllSessions: 3, ChangeTargetSession: 4, ChangeTargetAllTraces: 5, ChangeTargetTrace: 6, ChangeTargetPlanUsage: 7}
	return order[kind]
}

type ProjectionChangeWindow struct {
	From    ProjectionPosition
	Through ProjectionPosition
	Targets []ChangeTarget
}

type ProjectionChangeReader interface {
	CurrentProjectionPosition(context.Context) (ProjectionPosition, error)
	ReadProjectionChanges(context.Context, ProjectionPosition, int, int) (ProjectionChangeWindow, error)
	WaitForProjectionChange(context.Context, ProjectionPosition) error
}

type ActivityMutationOperation string

const (
	ActivityMutationUpsert ActivityMutationOperation = "upsert"
	ActivityMutationRemove ActivityMutationOperation = "remove"
)

type ActivityMutation struct {
	Operation  ActivityMutationOperation
	ActivityID string
	Activity   *Activity
}

type ActivitySyncFilter struct {
	After   ProjectionPosition
	Through ProjectionPosition
	Page    Page
}

type SessionActivitySyncFilter struct {
	ActivitySyncFilter
	Identity ConversationIdentity
}

type TraceActivitySyncFilter struct {
	ActivitySyncFilter
	TraceID TraceID
}

type ActivitySyncPage struct {
	Mutations  []ActivityMutation
	Through    ProjectionPosition
	Offset     int
	NextOffset int
	HasMore    bool
}

type ActivitySyncReader interface {
	SyncSessionActivities(context.Context, SessionActivitySyncFilter) (ActivitySyncPage, error)
	SyncTraceActivities(context.Context, TraceActivitySyncFilter) (ActivitySyncPage, error)
}

func ValidateProjectionPosition(current, requested ProjectionPosition) error {
	if requested.Generation != current.Generation {
		return ErrProjectionGeneration
	}
	if requested.Sequence > current.Sequence {
		return fmt.Errorf("%w: requested sequence is ahead", ErrProjectionCursorInvalid)
	}
	return nil
}
