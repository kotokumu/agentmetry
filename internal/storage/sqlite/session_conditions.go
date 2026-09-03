package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kotokumu/agentmetry/internal/query"
)

// matchSessionConditions streams the metadata needed by the predicates across
// every canonical member before paging. It does not read content/body columns;
// observed-outcome interpretation may inspect stored attributes.
func matchSessionConditions(ctx context.Context, reader sqlReader, filter query.SessionListFilter) (map[sessionRef]struct{}, error) {
	rows, err := reader.QueryContext(ctx, `WITH activity AS (
  SELECT source, run_id, name, status, model, tool_name, started_at, ended_at,
    CASE WHEN ? THEN attributes_json ELSE '{}' END AS attributes_json
  FROM spans WHERE activity_kind <> 'unknown' AND run_id <> '' AND (? = '' OR source = ?)
  UNION ALL
  SELECT source, run_id, name, '', model, tool_name, observed_at, observed_at,
    CASE WHEN ? THEN attributes_json ELSE '{}' END
  FROM logs WHERE activity_kind <> 'unknown' AND run_id <> '' AND (? = '' OR source = ?)
)
SELECT a.source, COALESCE(m.root_session_id, a.run_id), a.name, a.status,
  a.model, a.tool_name, a.started_at, a.ended_at, a.attributes_json
FROM activity a
LEFT JOIN session_memberships m ON m.source = a.source AND m.session_id = a.run_id`,
		filter.Conditions.ObservedFailure, filter.SourceID, filter.SourceID, filter.Conditions.ObservedFailure, filter.SourceID, filter.SourceID)
	if err != nil {
		return nil, fmt.Errorf("query session condition evidence: %w", err)
	}
	defer rows.Close()
	type evidence struct {
		first, last          time.Time
		invalidTime          bool
		failure, model, tool bool
	}
	grouped := make(map[sessionRef]evidence)
	conditions := filter.Conditions
	for rows.Next() {
		var ref sessionRef
		var activity query.Activity
		var model, tool, started, ended, attributes string
		if err := rows.Scan(&ref.sourceID, &ref.sessionID, &activity.Name, &activity.Status, &model, &tool, &started, &ended, &attributes); err != nil {
			return nil, fmt.Errorf("scan session condition evidence: %w", err)
		}
		value := grouped[ref]
		value.model = value.model || model == conditions.Model
		value.tool = value.tool || tool == conditions.Tool
		if conditions.ObservedFailure && !value.failure {
			if err := json.Unmarshal([]byte(attributes), &activity.Attributes); err != nil {
				return nil, fmt.Errorf("decode session outcome evidence: %w", err)
			}
			value.failure = query.ActivityHasObservedFailure(activity)
		}
		if conditions.MinDurationMS != nil || conditions.MaxDurationMS != nil {
			activityStart, startErr := parseStorageTime(started)
			activityEnd, endErr := parseStorageTime(ended)
			if startErr != nil || endErr != nil || !activityStart.After(time.Unix(0, 0)) || !activityEnd.After(time.Unix(0, 0)) || activityEnd.Before(activityStart) {
				value.invalidTime = true
			} else {
				if value.first.IsZero() || activityStart.Before(value.first) {
					value.first = activityStart
				}
				if value.last.IsZero() || activityEnd.After(value.last) {
					value.last = activityEnd
				}
			}
		}
		grouped[ref] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session condition evidence: %w", err)
	}
	matched := make(map[sessionRef]struct{})
	for ref, value := range grouped {
		if conditions.ObservedFailure && !value.failure || conditions.Model != "" && !value.model || conditions.Tool != "" && !value.tool {
			continue
		}
		if conditions.MinDurationMS != nil || conditions.MaxDurationMS != nil {
			if value.invalidTime || value.first.IsZero() || value.last.IsZero() {
				continue
			}
			elapsed := float64(value.last.Sub(value.first)) / float64(time.Millisecond)
			if conditions.MinDurationMS != nil && elapsed < *conditions.MinDurationMS || conditions.MaxDurationMS != nil && elapsed > *conditions.MaxDurationMS {
				continue
			}
		}
		matched[ref] = struct{}{}
	}
	return matched, nil
}
