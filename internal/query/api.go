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
	View       SessionListView
	Since      time.Time
	SourceID   string
	Search     string
	SessionID  string
	Page       Page
	Conditions SessionConditions
}

type SessionPage struct {
	Sessions          []SessionListEntry
	AppliedView       SessionListView
	NextOffset        int
	HasMore           bool
	AppliedConditions *SessionConditions
}

type ActivityPageFilter struct {
	Identity  ConversationIdentity
	AgentID   string
	Page      Page
	Direction TimelineDirection
	Anchor    ActivityAnchor
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
	GetSessionSummary(context.Context, ConversationIdentity) (Session, error)
}

type SessionActivitiesReader interface {
	ListSessionActivities(context.Context, ActivityPageFilter) (ActivityPage, error)
}
