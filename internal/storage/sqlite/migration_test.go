package sqlite

import (
	"context"
	"database/sql"
	_ "embed"
	"path/filepath"
	"testing"

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
