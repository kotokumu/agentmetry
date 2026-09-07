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

func (store *Store) populateSessionNames(ctx context.Context, reader sqlReader, sessions []query.SessionListEntry) error {
	ids := make([]string, 0, len(sessions))
	for _, session := range sessions {
		if session.SourceID == "claude" {
			ids = append(ids, session.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return fmt.Errorf("encode session name identities: %w", err)
	}
	rows, err := reader.QueryContext(ctx, sessionNameCandidatesSQL, string(encoded))
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
		observation, ok := store.profiles.SessionName("claude", event)
		if !ok || observation.ConversationID != id {
			continue
		}
		observations[id] = append(observations[id], query.SessionName{
			Text: observation.Text, Origin: observation.Origin, ObservedAt: observation.ObservedAt,
		})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate session name observations: %w", err)
	}
	for index := range sessions {
		if sessions[index].SourceID == "claude" {
			sessions[index].Name = query.SelectSessionName(observations[sessions[index].ID])
		}
	}
	return nil
}
