package query

import "errors"

var ErrInvalidSessionListView = errors.New("invalid session list view")

// SessionListView selects the unit of aggregation, filtering and pagination.
type SessionListView string

const (
	SessionListRoots SessionListView = "roots"
	SessionListAll   SessionListView = "all"
)

func ParseSessionListView(value string) (SessionListView, error) {
	switch SessionListView(value) {
	case "", SessionListRoots:
		return SessionListRoots, nil
	case SessionListAll:
		return SessionListAll, nil
	}
	return "", ErrInvalidSessionListView
}

type SessionRole string

const (
	SessionRoot  SessionRole = "root"
	SessionChild SessionRole = "child"
)

// SessionListEntry is a read projection, not a second conversation identity.
// Related IDs inherit Session.SourceID. Membership is resolved by the reader.
type SessionListEntry struct {
	Session
	RootSessionID   string
	ParentSessionID string
}

func (entry SessionListEntry) Role() SessionRole {
	if entry.ParentSessionID != "" {
		return SessionChild
	}
	return SessionRoot
}
