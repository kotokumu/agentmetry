package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/theoden9014/agentmetry/internal/planusage"
	"github.com/theoden9014/agentmetry/internal/query"
)

func (store *Store) PutPlanUsage(ctx context.Context, snapshot planusage.Snapshot) error {
	if err := snapshot.Validate(); err != nil {
		return err
	}
	var resetsAt any
	if snapshot.ResetsAt != nil {
		resetsAt = formatTime(*snapshot.ResetsAt)
	}
	raw := snapshot.Raw
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	store.writeMu.Lock()
	defer store.writeMu.Unlock()
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin plan usage commit: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	result, err := transaction.ExecContext(ctx, `INSERT OR IGNORE INTO plan_usage_snapshots (
  source, account_id, plan, window_id, window_duration_minutes, used_percent,
  resets_at, captured_at, authority, raw_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		snapshot.Source, snapshot.AccountID, snapshot.Plan, snapshot.WindowID,
		snapshot.WindowDurationMinutes, snapshot.UsedPercent, resetsAt,
		formatTime(snapshot.CapturedAt), snapshot.Authority, string(raw),
	)
	if err != nil {
		return fmt.Errorf("insert plan usage snapshot: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read plan usage result: %w", err)
	}
	var sequence int64
	if changed > 0 {
		sequence, err = appendProjectionChange(ctx, transaction, []query.ChangeTarget{query.OverviewTarget(), query.PlanUsageTarget(snapshot.Source)})
		if err != nil {
			return err
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit plan usage snapshot: %w", err)
	}
	if sequence > 0 {
		store.signalProjectionChange()
	}
	return nil
}

func (store *Store) LatestPlanUsage(ctx context.Context) ([]planusage.Snapshot, error) {
	rows, err := store.readDB.QueryContext(ctx, `SELECT source, account_id, plan, window_id,
  window_duration_minutes, used_percent, resets_at, captured_at, authority, raw_json
FROM plan_usage_snapshots AS current
WHERE NOT EXISTS (
  SELECT 1 FROM plan_usage_snapshots AS newer
  WHERE newer.source = current.source
    AND newer.account_id = current.account_id
    AND newer.window_id = current.window_id
    AND newer.captured_at > current.captured_at
)
ORDER BY source, window_duration_minutes, window_id`)
	if err != nil {
		return nil, fmt.Errorf("query latest plan usage: %w", err)
	}
	defer rows.Close()
	var snapshots []planusage.Snapshot
	for rows.Next() {
		var snapshot planusage.Snapshot
		var resetsAt sql.NullString
		var capturedAt, raw string
		if err := rows.Scan(
			&snapshot.Source, &snapshot.AccountID, &snapshot.Plan, &snapshot.WindowID,
			&snapshot.WindowDurationMinutes, &snapshot.UsedPercent, &resetsAt,
			&capturedAt, &snapshot.Authority, &raw,
		); err != nil {
			return nil, fmt.Errorf("scan plan usage snapshot: %w", err)
		}
		snapshot.CapturedAt, err = time.Parse(time.RFC3339Nano, capturedAt)
		if err != nil {
			return nil, fmt.Errorf("parse plan usage capture time: %w", err)
		}
		if resetsAt.Valid {
			value, err := time.Parse(time.RFC3339Nano, resetsAt.String)
			if err != nil {
				return nil, fmt.Errorf("parse plan usage reset time: %w", err)
			}
			snapshot.ResetsAt = &value
		}
		snapshot.Raw = []byte(raw)
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate plan usage snapshots: %w", err)
	}
	return snapshots, nil
}
