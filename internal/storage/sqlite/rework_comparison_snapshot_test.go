package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/query"
)

// The connector preserves the real SQLite driver and only schedules a writer
// at the boundary between two fixture identities. It neither matches SQL text
// nor counts queries, so summary/activity/harness reads may be reorganized.
type comparisonSnapshotConnector struct {
	driver      driver.Driver
	dsn         string
	beforeQuery func(context.Context, []driver.NamedValue) error
}

func (connector *comparisonSnapshotConnector) Connect(context.Context) (driver.Conn, error) {
	connection, err := connector.driver.Open(connector.dsn)
	if err != nil {
		return nil, err
	}
	return &comparisonSnapshotConnection{Conn: connection, beforeQuery: connector.beforeQuery}, nil
}

func (connector *comparisonSnapshotConnector) Driver() driver.Driver { return connector.driver }

type comparisonSnapshotConnection struct {
	driver.Conn
	beforeQuery func(context.Context, []driver.NamedValue) error
}

func (connection *comparisonSnapshotConnection) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	return connection.Conn.(driver.ConnBeginTx).BeginTx(ctx, options)
}

func (connection *comparisonSnapshotConnection) QueryContext(ctx context.Context, statement string, args []driver.NamedValue) (driver.Rows, error) {
	if err := connection.beforeQuery(ctx, args); err != nil {
		return nil, err
	}
	return connection.Conn.(driver.QueryerContext).QueryContext(ctx, statement, args)
}

func TestCompareReworkKeepsOneSnapshotAcrossConcurrentCommit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "comparison.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	start := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	var initial, arriving []canonical.Span
	for index, runID := range []string{"snapshot-before", "snapshot-after"} {
		startedAt := start.Add(time.Duration(index) * time.Hour)
		for step, operation := range []struct {
			name   string
			tool   string
			status string
		}{
			{name: "failed validation", tool: "exec_command", status: "Error"},
			{name: "edit", tool: "apply_patch", status: "Ok"},
			{name: "successful retry", tool: "exec_command", status: "Ok"},
		} {
			attributes := map[string]any{"gen_ai.usage.role": "authoritative_call"}
			if operation.tool == "apply_patch" {
				attributes["file_path"] = "main.go"
			} else {
				attributes["command"] = "go test ./..."
			}
			initial = append(initial, canonical.Span{
				Source: "codex", TraceID: fmt.Sprintf("%032x", index+1), SpanID: fmt.Sprintf("%016x", step+1),
				Name: operation.name, Kind: canonical.ActivityTool, ToolName: operation.tool, Status: operation.status,
				StartedAt: startedAt.Add(time.Duration(step*2) * time.Second), EndedAt: startedAt.Add(time.Duration(step*2+1) * time.Second),
				Attributes: attributes,
				Agent:      canonical.AgentContext{RunID: runID, AgentID: "worker", Tokens: canonical.TokenUsage{Input: int64((step + 1) * 10), Presence: canonical.TokenPresence{Output: true}}},
			})
		}
		arriving = append(arriving, canonical.Span{
			Source: "codex", TraceID: fmt.Sprintf("%032x", index+1), SpanID: "0000000000000004",
			Name: "later validation", Kind: canonical.ActivityTool, ToolName: "exec_command", Status: "Error",
			StartedAt: startedAt.Add(6 * time.Second), EndedAt: startedAt.Add(8 * time.Second),
			Attributes: map[string]any{"command": "go test ./...", "gen_ai.usage.role": "authoritative_call"},
			Agent:      canonical.AgentContext{RunID: runID, AgentID: "worker", Tokens: canonical.TokenUsage{Input: 40, Presence: canonical.TokenPresence{Output: true}}},
		})
	}
	if err := store.CommitBatch(ctx, canonical.Batch{Signal: canonical.SignalTrace, Spans: initial}); err != nil {
		t.Fatal(err)
	}
	baseline, err := query.NewConversationIdentity("codex", "snapshot-before")
	if err != nil {
		t.Fatal(err)
	}
	current, err := query.NewConversationIdentity("codex", "snapshot-after")
	if err != nil {
		t.Fatal(err)
	}
	pair := query.ReworkComparisonPair{Baseline: baseline, Current: current}
	want, err := store.CompareRework(ctx, pair)
	if err != nil {
		t.Fatal(err)
	}

	// The pre-commit public result is the temporal oracle, not an arithmetic
	// oracle. Fixed before/after facts below prove that the committed update is
	// observable in every snapshot dimension relevant to this test.
	harnessEqual := cmp.Comparer(func(left, right query.HarnessContext) bool {
		leftView, leftErr := query.InspectHarnessContext(left)
		rightView, rightErr := query.InspectHarnessContext(right)
		return leftErr == nil && rightErr == nil && cmp.Equal(leftView, rightView)
	})
	var boundary sync.Mutex
	var firstSubject string
	committed := false
	originalReadDB := store.readDB
	defer func() { _ = originalReadDB.Close() }()
	store.readDB = sql.OpenDB(&comparisonSnapshotConnector{
		driver: originalReadDB.Driver(), dsn: sqliteDSN(path, true),
		beforeQuery: func(ctx context.Context, args []driver.NamedValue) error {
			boundary.Lock()
			defer boundary.Unlock()
			for _, argument := range args {
				subject, ok := argument.Value.(string)
				if !ok || (subject != baseline.ConversationID() && subject != current.ConversationID()) {
					continue
				}
				if firstSubject == "" {
					firstSubject = subject
				}
				if subject != firstSubject && !committed {
					if err := store.CommitBatch(ctx, canonical.Batch{Signal: canonical.SignalTrace, Spans: arriving}); err != nil {
						return fmt.Errorf("commit between comparison subjects: %w", err)
					}
					committed = true
				}
			}
			return nil
		},
	})
	got, err := store.CompareRework(ctx, pair)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(true, committed); diff != "" {
		t.Fatalf("writer did not commit between subjects (-want +got): %s", diff)
	}
	if diff := cmp.Diff(want, got, harnessEqual); diff != "" {
		t.Errorf("comparison mixed committed snapshots (-want +got):\n%s", diff)
	}
	after, err := store.CompareRework(ctx, pair)
	if err != nil {
		t.Fatal(err)
	}
	for _, observation := range []struct {
		name       string
		comparison query.ReworkComparison
		endOffset  time.Duration
		tokens     float64
		activities int64
	}{
		{name: "before", comparison: want, endOffset: 5 * time.Second, tokens: 60, activities: 3},
		{name: "after", comparison: after, endOffset: 8 * time.Second, tokens: 100, activities: 4},
	} {
		t.Run(observation.name, func(t *testing.T) {
			if diff := cmp.Diff("ready", observation.comparison.Status); diff != "" {
				t.Fatalf("comparison status (-want +got): %s", diff)
			}
			for index, summary := range []query.ReworkComparisonSummary{observation.comparison.Baseline, observation.comparison.Current} {
				if diff := cmp.Diff(start.Add(time.Duration(index)*time.Hour+time.Second), summary.StartedAt); diff != "" {
					t.Errorf("subject %d start (-want +got): %s", index, diff)
				}
				if diff := cmp.Diff(start.Add(time.Duration(index)*time.Hour+observation.endOffset), summary.EndedAt); diff != "" {
					t.Errorf("subject %d end (-want +got): %s", index, diff)
				}
				if diff := cmp.Diff(observation.activities, summary.Coverage.CanonicalEvents); diff != "" {
					t.Errorf("subject %d analyzed count (-want +got): %s", index, diff)
				}
				if diff := cmp.Diff("complete", summary.ProjectionCoverage); diff != "" {
					t.Errorf("subject %d projection coverage (-want +got): %s", index, diff)
				}
			}
			var tokenShare *query.ReworkComparisonRow
			for index := range observation.comparison.Rows {
				if observation.comparison.Rows[index].ID == "rework_token_share" {
					tokenShare = &observation.comparison.Rows[index]
				}
			}
			if tokenShare == nil {
				t.Fatal("comparison omitted rework token share")
			}
			for index, value := range []query.ReworkComparisonValue{tokenShare.Baseline, tokenShare.Current} {
				if diff := cmp.Diff(&observation.tokens, value.Denominator); diff != "" {
					t.Errorf("subject %d snapshot token denominator (-want +got): %s", index, diff)
				}
			}
		})
	}
}
