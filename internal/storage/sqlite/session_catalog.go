package sqlite

import "github.com/kotokumu/agentmetry/internal/query"

// sessionListUnitID is shared by activity predicates and rollup aggregation.
// alias is always a local SQL alias, never caller input.
func sessionListUnitID(view query.SessionListView, alias string) string {
	if view == query.SessionListAll {
		return alias + ".run_id"
	}
	return "COALESCE(m.root_session_id, " + alias + ".run_id)"
}
