package connectapi

import (
	v1 "github.com/kotokumu/agentmetry/gen/agentmetry/v1"
	"github.com/kotokumu/agentmetry/internal/query"
)

func sessionListView(value v1.SessionListView) (query.SessionListView, error) {
	switch value {
	case v1.SessionListView_SESSION_LIST_VIEW_UNSPECIFIED, v1.SessionListView_SESSION_LIST_VIEW_ROOTS:
		return query.SessionListRoots, nil
	case v1.SessionListView_SESSION_LIST_VIEW_ALL:
		return query.SessionListAll, nil
	default:
		return "", query.ErrInvalidSessionListView
	}
}

func mapSessionListView(value query.SessionListView) v1.SessionListView {
	switch value {
	case query.SessionListRoots:
		return v1.SessionListView_SESSION_LIST_VIEW_ROOTS
	case query.SessionListAll:
		return v1.SessionListView_SESSION_LIST_VIEW_ALL
	default:
		return v1.SessionListView_SESSION_LIST_VIEW_UNSPECIFIED
	}
}
