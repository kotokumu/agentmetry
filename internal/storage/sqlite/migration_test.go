package sqlite

import (
	"context"
	"database/sql"
	_ "embed"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ariga.io/atlas/sql/schema"
	"github.com/google/go-cmp/cmp"
	_ "modernc.org/sqlite"
)

func Test_convergeSchema(t *testing.T) {
	type args struct {
		ctx      context.Context
		database *sql.DB
	}
	emptyDatabase := must(sql.Open("sqlite", filepath.Join(t.TempDir(), "empty.db")))
	legacyDatabase := must(sql.Open("sqlite", filepath.Join(t.TempDir(), "legacy.db")))
	must(legacyDatabase.Exec(`
CREATE TABLE logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  observed_at TEXT NOT NULL,
  severity TEXT NOT NULL,
  name TEXT NOT NULL,
  body TEXT NOT NULL,
  trace_id TEXT NOT NULL,
  span_id TEXT NOT NULL,
  activity_kind TEXT NOT NULL,
  tool_name TEXT NOT NULL,
  target_agent_id TEXT NOT NULL,
  agent_id TEXT NOT NULL,
  agent_type TEXT NOT NULL,
  parent_agent_id TEXT NOT NULL,
  run_id TEXT NOT NULL,
  model TEXT NOT NULL,
  cost_usd REAL,
  attributes_json TEXT NOT NULL
);
CREATE INDEX logs_observed_at_idx ON logs(observed_at);
INSERT INTO logs (
  observed_at, severity, name, body, trace_id, span_id, activity_kind,
  tool_name, target_agent_id, agent_id, agent_type, parent_agent_id,
  run_id, model, attributes_json
) VALUES (
  '2026-08-11T00:00:00Z', 'INFO', 'legacy', 'retained', '', '', '',
  '', '', '', '', '', '', '', '{}'
);`))
	t.Cleanup(func() {
		_ = emptyDatabase.Close()
		_ = legacyDatabase.Close()
	})

	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "creates the desired schema for a new database",
			args: args{ctx: context.Background(), database: emptyDatabase},
		},
		{
			name: "adds safe columns without replacing legacy rows",
			args: args{ctx: context.Background(), database: legacyDatabase},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := convergeSchema(tt.args.ctx, tt.args.database); (err != nil) != tt.wantErr {
				t.Errorf("convergeSchema() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}

	var body, targetAgentType string
	var inputTokens int64
	if err := legacyDatabase.QueryRow(
		"SELECT body, target_agent_type, input_tokens FROM logs WHERE name = 'legacy'",
	).Scan(&body, &targetAgentType, &inputTokens); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]any{"retained", "", int64(0)}, []any{body, targetAgentType, inputTokens}); diff != "" {
		t.Errorf("legacy row mismatch (-want +got):\n%s", diff)
	}
}

func TestCodexNameIndexMigration(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "index.db")
	database := must(sql.Open("sqlite", path))
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	if err := convergeSchema(ctx, database); err != nil {
		t.Fatal(err)
	}
	// Remove only the new index to reproduce the prior schema, retaining data.
	must(database.Exec(`DROP INDEX IF EXISTS logs_codex_session_names_idx`))
	must(database.Exec(`PRAGMA user_version = 42`))
	must(database.Exec(`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<100000)
INSERT INTO logs (source,observed_at,severity,name,body,trace_id,span_id,activity_kind,tool_name,target_agent_id,agent_id,agent_type,parent_agent_id,run_id,model,attributes_json)
SELECT 'codex','2026-09-09T00:00:00Z','INFO','gen_ai.tool_result','retained','','','tool',CASE WHEN x%10000=0 THEN 'list_threads' ELSE 'exec_command' END,'','','','','executor','','{}' FROM n`))
	must(database.Exec(`PRAGMA query_only=ON`))
	err := convergeSchema(ctx, database)
	if diff := cmp.Diff(true, err != nil); diff != "" {
		t.Fatalf("write denial must fail index creation: %s", diff)
	}
	var indexes, count, generation int
	if err := database.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='logs_codex_session_names_idx'`).Scan(&indexes); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(0, indexes); diff != "" {
		t.Fatal(diff)
	}
	if err := database.QueryRow(`SELECT count(*) FROM logs WHERE body='retained'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`PRAGMA user_version`).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]int{100000, 42}, []int{count, generation}); diff != "" {
		t.Fatal(diff)
	}
	must(database.Exec(`PRAGMA query_only=OFF`))
	started := time.Now()
	if err := convergeSchema(ctx, database); err != nil {
		t.Fatal(err)
	}
	t.Logf("100000 logs / 10 candidates: schema convergence and index build %s", time.Since(started))
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	database = must(sql.Open("sqlite", path))
	if err := verifyConverged(ctx, database, must(evaluateDesiredSchema())); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='logs_codex_session_names_idx'`).Scan(&indexes); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(1, indexes); diff != "" {
		t.Fatal(diff)
	}
	if err := database.QueryRow(`SELECT count(*) FROM logs WHERE body='retained'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`PRAGMA user_version`).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]int{100000, 42}, []int{count, generation}); diff != "" {
		t.Fatal(diff)
	}
	plan := must(database.QueryContext(ctx, "EXPLAIN QUERY PLAN "+codexSessionNameCandidatesSQL))
	var details []string
	for plan.Next() {
		var id, parent, unused int
		var detail string
		if err := plan.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		details = append(details, detail)
	}
	if err := plan.Err(); err != nil {
		t.Fatal(err)
	}
	_ = plan.Close()
	if diff := cmp.Diff(true, strings.Contains(strings.Join(details, " "), "logs_codex_session_names_idx")); diff != "" {
		t.Fatalf("partial index plan %v: %s", details, diff)
	}
	started = time.Now()
	rows := must(database.QueryContext(ctx, codexSessionNameCandidatesSQL))
	var candidates []string
	for rows.Next() {
		var executor, name, attrs string
		if err := rows.Scan(&executor, &name, &attrs); err != nil {
			t.Fatal(err)
		}
		candidates = append(candidates, executor)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	if diff := cmp.Diff(10, len(candidates)); diff != "" {
		t.Fatal(diff)
	}
	t.Logf("candidate read %s; plan %v", time.Since(started), details)
}

func TestExtraIndexDowngradeRules(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "downgrade.db")
	database := must(sql.Open("sqlite", path))
	t.Cleanup(func() { _ = database.Close() })
	if err := convergeSchema(ctx, database); err != nil {
		t.Fatal(err)
	}
	// Characterize the same DropIndex difference an older desired schema sees;
	// this is not execution of an old binary or of its full journal replay.
	must(database.Exec(`CREATE INDEX future_codex_name_idx ON logs(id) WHERE source='codex' AND tool_name='list_threads'`))
	rebuild, err := RequiresProjectionRebuild(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(true, rebuild); diff != "" {
		t.Fatal(diff)
	}
	_, err = Open(path)
	if diff := cmp.Diff(true, err != nil); diff != "" {
		t.Fatalf("low-level Open must reject DropIndex: %s", diff)
	}
}

func Test_evaluateDesiredSchema(t *testing.T) {
	tests := []struct {
		name    string
		want    *schema.Schema
		wantErr bool
	}{
		{
			name: "loads the complete Agentmetry schema",
			want: schema.New("main").AddTables(
				schema.NewTable("spans"),
				schema.NewTable("session_rollups"),
				schema.NewTable("session_links"),
				schema.NewTable("session_memberships"),
				schema.NewTable("session_agents"),
				schema.NewTable("session_traces"),
				schema.NewTable("trace_rollups"),
				schema.NewTable("trace_conversations"),
				schema.NewTable("trace_agents"),
				schema.NewTable("logs"),
				schema.NewTable("metrics"),
				schema.NewTable("projection_feed_state"),
				schema.NewTable("projection_changes"),
				schema.NewTable("activity_changes"),
				schema.NewTable("otlp_exports"),
				schema.NewTable("observations"),
				schema.NewTable("plan_usage_snapshots"),
				schema.NewTable("model_rates"),
				schema.NewTable("model_calls"),
				schema.NewTable("model_call_evidence"),
				schema.NewTable("model_call_evidence_aliases"),
				schema.NewTable("model_call_attributions"),
				schema.NewTable("model_call_activity_links"),
				schema.NewTable("model_call_trace_memberships"),
				schema.NewTable("model_call_trace_supports"),
				schema.NewTable("retention_policy"),
				schema.NewTable("retained_exports"),
				schema.NewTable("archive_segments"),
				schema.NewTable("archive_segment_members"),
				schema.NewTable("current_archive_memberships"),
				schema.NewTable("archive_segment_replacements"),
				schema.NewTable("retention_cycles"),
				schema.NewTable("retention_cycle_segments"),
				schema.NewTable("retention_operations"),
				schema.NewTable("retention_operation_exports"),
				schema.NewTable("retention_export_authorities"),
				schema.NewTable("span_projection_candidates"),
				schema.NewTable("session_link_evidence"),
			),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := evaluateDesiredSchema()
			if (err != nil) != tt.wantErr {
				t.Fatalf("evaluateDesiredSchema() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			wantNames := make([]string, 0, len(tt.want.Tables))
			for _, table := range tt.want.Tables {
				wantNames = append(wantNames, table.Name)
			}
			gotNames := make([]string, 0, len(got.Tables))
			for _, table := range got.Tables {
				gotNames = append(gotNames, table.Name)
			}
			if diff := cmp.Diff(tt.want.Name, got.Name); diff != "" {
				t.Errorf("schema name mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(wantNames, gotNames); diff != "" {
				t.Errorf("table names mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func Test_verifyConverged(t *testing.T) {
	type args struct {
		ctx      context.Context
		database *sql.DB
		desired  *schema.Schema
	}
	convergedDatabase := must(sql.Open("sqlite", filepath.Join(t.TempDir(), "converged.db")))
	emptyDatabase := must(sql.Open("sqlite", filepath.Join(t.TempDir(), "empty.db")))
	if err := convergeSchema(context.Background(), convergedDatabase); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = convergedDatabase.Close()
		_ = emptyDatabase.Close()
	})
	desired := must(evaluateDesiredSchema())

	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "accepts an already converged database",
			args: args{ctx: context.Background(), database: convergedDatabase, desired: desired},
		},
		{
			name:    "rejects a database with remaining changes",
			args:    args{ctx: context.Background(), database: emptyDatabase, desired: desired},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := verifyConverged(tt.args.ctx, tt.args.database, tt.args.desired); (err != nil) != tt.wantErr {
				t.Errorf("verifyConverged() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_validateAutomaticChanges(t *testing.T) {
	type args struct {
		changes []schema.Change
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "accepts no changes",
			args: args{changes: nil},
		},
		{
			name: "accepts a safe change list",
			args: args{changes: []schema.Change{&schema.AddTable{T: schema.NewTable("events")}}},
		},
		{
			name:    "rejects a list containing a destructive change",
			args:    args{changes: []schema.Change{&schema.AddTable{T: schema.NewTable("events")}, &schema.DropTable{T: schema.NewTable("logs")}}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateAutomaticChanges(tt.args.changes); (err != nil) != tt.wantErr {
				t.Errorf("validateAutomaticChanges() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_validateAutomaticChange(t *testing.T) {
	type args struct {
		change schema.Change
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "accepts a new table",
			args: args{change: &schema.AddTable{T: schema.NewTable("events")}},
		},
		{
			name: "accepts a new index",
			args: args{change: &schema.AddIndex{I: schema.NewIndex("events_time_idx")}},
		},
		{
			name: "accepts a nullable column",
			args: args{change: &schema.ModifyTable{
				T: schema.NewTable("events"),
				Changes: []schema.Change{&schema.AddColumn{
					C: schema.NewNullStringColumn("detail", "text"),
				}},
			}},
		},
		{
			name: "accepts a non-null column with a default",
			args: args{change: &schema.ModifyTable{
				T: schema.NewTable("events"),
				Changes: []schema.Change{&schema.AddColumn{
					C: schema.NewIntColumn("attempt", "integer").SetDefault(&schema.Literal{V: "0"}),
				}},
			}},
		},
		{
			name: "rejects a non-null column without a default",
			args: args{change: &schema.ModifyTable{
				T: schema.NewTable("events"),
				Changes: []schema.Change{&schema.AddColumn{
					C: schema.NewStringColumn("detail", "text"),
				}},
			}},
			wantErr: true,
		},
		{
			name:    "rejects dropping a table",
			args:    args{change: &schema.DropTable{T: schema.NewTable("logs")}},
			wantErr: true,
		},
		{
			name: "rejects a table rebuild change",
			args: args{change: &schema.ModifyTable{
				T: schema.NewTable("events"),
				Changes: []schema.Change{&schema.DropColumn{
					C: schema.NewStringColumn("detail", "text"),
				}},
			}},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateAutomaticChange(tt.args.change); (err != nil) != tt.wantErr {
				t.Errorf("validateAutomaticChange() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}
