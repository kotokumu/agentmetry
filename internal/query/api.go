package query

import (
	"context"
	"time"
)

type DashboardFilter struct {
	Since    time.Time
	SourceID string
	Search   string
}

type SessionListFilter struct {
	Since     time.Time
	SourceID  string
	Search    string
	SessionID string
	PageSize  int
	Offset    int
}

type SessionPage struct {
	Sessions   []Session
	NextOffset int
	HasMore    bool
}

type ActivityPageFilter struct {
	SourceID       string
	ConversationID string
	AgentID        string
	PageSize       int
	Offset         int
	Direction      string
	TraceID        string
	SpanID         string
}

type ActivityPage struct {
	Activities []Activity
	Total      int64
	Offset     int
	HasEarlier bool
	HasMore    bool
}

type DashboardReader interface {
	GetDashboard(context.Context, DashboardFilter) (Overview, error)
}

type SessionListReader interface {
	ListSessions(context.Context, SessionListFilter) (SessionPage, error)
}

type SessionSummaryReader interface {
	GetSessionSummary(context.Context, string, string) (Session, error)
}

type SessionActivitiesReader interface {
	ListSessionActivities(context.Context, ActivityPageFilter) (ActivityPage, error)
}
