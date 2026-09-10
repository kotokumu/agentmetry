package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func Test_rebuildCodexCorroboratingSupports(t *testing.T) {
	type args struct {
		ctx         context.Context
		transaction *sql.Tx
		sequence    int64
		sessionID   string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "empty session is ignored without accessing storage",
			args: args{
				ctx:       context.Background(),
				sequence:  1,
				sessionID: "",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := rebuildCodexCorroboratingSupports(tt.args.ctx, tt.args.transaction, tt.args.sequence, tt.args.sessionID); (err != nil) != tt.wantErr {
				t.Errorf("rebuildCodexCorroboratingSupports() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCodexCorroboratingCandidatesUseSessionUsageIndexes(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	rows, err := database.readDB.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+codexCorroboratingCandidatesSQL+`
SELECT activity_id, call_id, 'corroborating' FROM unique_candidates`, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var details []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	plan := strings.Join(details, "\n")
	for _, index := range []string{
		"logs_source_run_usage_idx",
		"spans_source_run_usage_idx",
	} {
		if !strings.Contains(plan, index) {
			t.Errorf("query plan does not use %s:\n%s", index, plan)
		}
	}
	if strings.Contains(plan, "spans_source_run_agent_parent_idx") {
		t.Errorf("query plan uses unbounded spans_source_run_agent_parent_idx:\n%s", plan)
	}
}
