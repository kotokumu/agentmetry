package sqlite

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kotokumu/agentmetry/internal/query"
	"github.com/kotokumu/agentmetry/sourceplugin"
)

// Candidate retrieval is limited to native Claude conversations on this page.
// Older observations remain relevant even when outside the activity date filter.
const sessionNameCandidatesSQL = `SELECT run_id, name, attributes_json FROM logs
WHERE source = 'claude' AND run_id IN (SELECT value FROM json_each(?))
  AND json_extract(attributes_json, '$.query_source') = 'generate_session_title'`

// Codex targets live inside other conversations' results, so executor and date
// filters would drop valid observations. The partial index bounds the scan to
// list results without imposing a lossy history cutoff.
const codexSessionNameCandidatesSQL = `SELECT run_id, name, attributes_json FROM logs INDEXED BY logs_codex_session_names_idx
WHERE source = 'codex' AND tool_name = 'list_threads'`

func (store *Store) populateSessionNames(ctx context.Context, reader sqlReader, sessions []query.SessionListEntry) error {
	for _, sourceID := range []string{"claude", "codex"} {
		if err := store.populateSourceSessionNames(ctx, reader, sessions, sourceID); err != nil {
			return err
		}
	}
	return nil
}

func (store *Store) populateSourceSessionNames(ctx context.Context, reader sqlReader, sessions []query.SessionListEntry, sourceID string) error {
	ids := make([]string, 0, len(sessions))
	targets := make(map[string]bool)
	for _, session := range sessions {
		if session.SourceID == sourceID {
			ids = append(ids, session.ID)
			targets[session.ID] = true
		}
	}
	if len(ids) == 0 {
		return nil
	}
	statement := codexSessionNameCandidatesSQL
	var args []any
	if sourceID == "claude" {
		encoded, err := json.Marshal(ids)
		if err != nil {
			return fmt.Errorf("encode session name identities: %w", err)
		}
		statement, args = sessionNameCandidatesSQL, []any{string(encoded)}
	}
	rows, err := reader.QueryContext(ctx, statement, args...)
	if err != nil {
		return fmt.Errorf("query session name observations: %w", err)
	}
	defer rows.Close()
	observations := make(map[string][]query.SessionName)
	for rows.Next() {
		var id, name, attributes string
		if err := rows.Scan(&id, &name, &attributes); err != nil {
			return fmt.Errorf("scan session name observation: %w", err)
		}
		event := sourceplugin.Event{Name: name}
		if err := json.Unmarshal([]byte(attributes), &event.Attributes); err != nil {
			continue
		}
		for _, observation := range store.profiles.SessionNames(sourceID, event) {
			if !targets[observation.ConversationID] || (sourceID == "claude" && observation.ConversationID != id) {
				continue
			}
			observations[observation.ConversationID] = append(observations[observation.ConversationID], query.SessionName{
				Text: observation.Text, Origin: observation.Origin, ObservedAt: observation.ObservedAt,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate session name observations: %w", err)
	}
	for index := range sessions {
		if sessions[index].SourceID == sourceID {
			sessions[index].Name = query.SelectSessionName(observations[sessions[index].ID])
		}
	}
	return nil
}
