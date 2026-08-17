package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/query"
)

type sessionRef struct {
	sourceID  string
	sessionID string
}

type sessionGraph struct {
	parentBySession map[sessionRef]sessionRef
	rootBySession   map[sessionRef]sessionRef
	membersByRoot   map[sessionRef][]sessionRef
}

func (store *Store) loadSessionGraph(ctx context.Context, sourceID string) (sessionGraph, error) {
	return loadSessionGraphWithReader(ctx, store.readDB, sourceID)
}

func loadSessionGraphWithReader(ctx context.Context, reader sqlReader, sourceID string) (sessionGraph, error) {
	rows, err := reader.QueryContext(ctx, `SELECT source, session_id, root_session_id, parent_session_id
FROM session_memberships
WHERE (? = '' OR source = ?)
ORDER BY source, session_id`, sourceID, sourceID)
	if err != nil {
		return sessionGraph{}, fmt.Errorf("query session delegation graph: %w", err)
	}
	return scanStoredSessionGraph(rows)
}

func (store *Store) loadSessionGroup(ctx context.Context, ref sessionRef) (sessionGraph, error) {
	return loadSessionGroupWithReader(ctx, store.readDB, ref)
}

func loadSessionGroupWithReader(ctx context.Context, reader sqlReader, ref sessionRef) (sessionGraph, error) {
	rows, err := reader.QueryContext(ctx, `SELECT source, session_id, root_session_id, parent_session_id
FROM session_memberships
WHERE source = ? AND root_session_id = COALESCE(
  (SELECT root_session_id FROM session_memberships WHERE source = ? AND session_id = ?), ?)
ORDER BY session_id`, ref.sourceID, ref.sourceID, ref.sessionID, ref.sessionID)
	if err != nil {
		return sessionGraph{}, fmt.Errorf("query session group: %w", err)
	}
	return scanStoredSessionGraph(rows)
}

func scanStoredSessionGraph(rows *sql.Rows) (sessionGraph, error) {
	defer rows.Close()

	graph := sessionGraph{
		parentBySession: make(map[sessionRef]sessionRef), rootBySession: make(map[sessionRef]sessionRef),
		membersByRoot: make(map[sessionRef][]sessionRef),
	}
	for rows.Next() {
		var source, sessionID, rootID, parentID string
		if err := rows.Scan(&source, &sessionID, &rootID, &parentID); err != nil {
			return sessionGraph{}, fmt.Errorf("scan session delegation graph: %w", err)
		}
		ref := sessionRef{sourceID: source, sessionID: sessionID}
		root := sessionRef{sourceID: source, sessionID: rootID}
		graph.rootBySession[ref] = root
		graph.membersByRoot[root] = append(graph.membersByRoot[root], ref)
		if parentID != "" {
			graph.parentBySession[ref] = sessionRef{sourceID: source, sessionID: parentID}
		}
	}
	if err := rows.Err(); err != nil {
		return sessionGraph{}, fmt.Errorf("iterate session delegation graph: %w", err)
	}

	for root := range graph.membersByRoot {
		sort.Slice(graph.membersByRoot[root], func(i, j int) bool {
			return graph.membersByRoot[root][i].sessionID < graph.membersByRoot[root][j].sessionID
		})
	}
	return graph, nil
}

func rebuildAffectedSessionMemberships(ctx context.Context, transaction *sql.Tx, batch canonical.Batch) error {
	sources := make(map[string]struct{})
	for _, link := range batch.SessionLinks {
		if link.ParentSessionID != "" && link.ChildSessionID != "" && link.ParentSessionID != link.ChildSessionID {
			sources[normalizeSource(link.Source)] = struct{}{}
		}
	}
	for sourceID := range sources {
		if err := rebuildSessionMemberships(ctx, transaction, sourceID); err != nil {
			return err
		}
	}
	return nil
}

func rebuildSessionMemberships(ctx context.Context, transaction *sql.Tx, sourceID string) error {
	rows, err := transaction.QueryContext(ctx, `SELECT parent_session_id, child_session_id FROM session_links WHERE source = ?`, sourceID)
	if err != nil {
		return fmt.Errorf("query session links: %w", err)
	}
	nodes := make(map[sessionRef]struct{})
	candidates := make(map[sessionRef]map[sessionRef]struct{})
	for rows.Next() {
		var parentID, childID string
		if err := rows.Scan(&parentID, &childID); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan session link: %w", err)
		}
		parent := sessionRef{sourceID: sourceID, sessionID: parentID}
		child := sessionRef{sourceID: sourceID, sessionID: childID}
		nodes[parent], nodes[child] = struct{}{}, struct{}{}
		if candidates[child] == nil {
			candidates[child] = make(map[sessionRef]struct{})
		}
		candidates[child][parent] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate session links: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close session links: %w", err)
	}
	graph := newSessionGraph(nodes, candidates)
	if _, err := transaction.ExecContext(ctx, `DELETE FROM session_memberships WHERE source = ?`, sourceID); err != nil {
		return fmt.Errorf("delete session memberships: %w", err)
	}
	for ref, root := range graph.rootBySession {
		parentID := ""
		if parent, exists := graph.parent(ref); exists {
			parentID = parent.sessionID
		}
		if _, err := transaction.ExecContext(ctx, `INSERT INTO session_memberships (source, session_id, root_session_id, parent_session_id) VALUES (?, ?, ?, ?)`,
			sourceID, ref.sessionID, root.sessionID, parentID); err != nil {
			return fmt.Errorf("store session membership: %w", err)
		}
	}
	return nil
}

func newSessionGraph(nodes map[sessionRef]struct{}, candidates map[sessionRef]map[sessionRef]struct{}) sessionGraph {
	parents := make(map[sessionRef]sessionRef)
	for child, possibleParents := range candidates {
		if len(possibleParents) != 1 {
			continue
		}
		for parent := range possibleParents {
			parents[child] = parent
		}
	}

	for node := range nodes {
		positions := make(map[sessionRef]int)
		path := make([]sessionRef, 0, 4)
		current := node
		for {
			if cycleStart, exists := positions[current]; exists {
				for _, cyclic := range path[cycleStart:] {
					delete(parents, cyclic)
				}
				break
			}
			positions[current] = len(path)
			path = append(path, current)
			parent, exists := parents[current]
			if !exists {
				break
			}
			current = parent
		}
	}

	graph := sessionGraph{
		parentBySession: parents,
		rootBySession:   make(map[sessionRef]sessionRef, len(nodes)),
		membersByRoot:   make(map[sessionRef][]sessionRef),
	}
	for node := range nodes {
		root := node
		for {
			parent, exists := parents[root]
			if !exists {
				break
			}
			root = parent
		}
		graph.rootBySession[node] = root
		graph.membersByRoot[root] = append(graph.membersByRoot[root], node)
	}
	for root := range graph.membersByRoot {
		sort.Slice(graph.membersByRoot[root], func(i, j int) bool {
			return graph.membersByRoot[root][i].sessionID < graph.membersByRoot[root][j].sessionID
		})
	}
	return graph
}

func (graph sessionGraph) root(ref sessionRef) sessionRef {
	if root, exists := graph.rootBySession[ref]; exists {
		return root
	}
	return ref
}

func (graph sessionGraph) members(ref sessionRef) []sessionRef {
	root := graph.root(ref)
	if members := graph.membersByRoot[root]; len(members) > 0 {
		return append([]sessionRef(nil), members...)
	}
	return []sessionRef{ref}
}

func (graph sessionGraph) parent(ref sessionRef) (sessionRef, bool) {
	parent, exists := graph.parentBySession[ref]
	return parent, exists
}

func (graph sessionGraph) grouped(ref sessionRef) bool {
	return len(graph.members(graph.root(ref))) > 1
}

func (graph sessionGraph) normalizeActivityAgent(activity query.Activity) query.Activity {
	ref := sessionRef{sourceID: activity.Source, sessionID: activity.RunID}
	if activity.RunID == "" {
		return activity
	}
	nativeAgentID := activity.AgentID
	activity.AgentID = graph.effectiveAgentID(ref, nativeAgentID)
	if activity.AgentType == "" && graph.root(ref) == ref && isPrimarySessionAgent(ref, nativeAgentID) {
		activity.AgentType = "root"
	}
	if activity.ParentAgentID != "" {
		activity.ParentAgentID = graph.effectiveParentAgentID(ref, activity.ParentAgentID)
	} else if graph.grouped(ref) && isPrimarySessionAgent(ref, nativeAgentID) {
		if parent, exists := graph.parent(ref); exists {
			activity.ParentAgentID = parent.sessionID
		}
	} else if graph.grouped(ref) && !isPrimarySessionAgent(ref, nativeAgentID) {
		activity.ParentAgentID = ref.sessionID
	}
	return activity
}

func (graph sessionGraph) effectiveAgentID(ref sessionRef, nativeAgentID string) string {
	if !graph.grouped(ref) {
		if isPrimarySessionAgent(ref, nativeAgentID) {
			return "main"
		}
		return sessionAgentID(nativeAgentID)
	}
	if isPrimarySessionAgent(ref, nativeAgentID) {
		return ref.sessionID
	}
	return ref.sessionID + "/" + nativeAgentID
}

func (graph sessionGraph) effectiveParentAgentID(ref sessionRef, nativeParentID string) string {
	if !graph.grouped(ref) {
		if isPrimarySessionAgent(ref, nativeParentID) {
			return "main"
		}
		return nativeParentID
	}
	if nativeParentID == "main" || nativeParentID == ref.sessionID {
		return ref.sessionID
	}
	for _, member := range graph.members(ref) {
		if nativeParentID == member.sessionID {
			return member.sessionID
		}
	}
	return ref.sessionID + "/" + nativeParentID
}

func isPrimarySessionAgent(ref sessionRef, nativeAgentID string) bool {
	return nativeAgentID == "" || nativeAgentID == "main" || nativeAgentID == ref.sessionID
}
