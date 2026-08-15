package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/theoden9014/agentmetry/internal/query"
)

type countingSQLReader struct {
	sqlReader
	queryCount int
}

func (reader *countingSQLReader) QueryContext(ctx context.Context, statement string, args ...any) (*sql.Rows, error) {
	reader.queryCount++
	return reader.sqlReader.QueryContext(ctx, statement, args...)
}

func TestInferAgentParentsBoundsEvidenceFreeAgentQueries(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	agents := make([]query.AgentSession, 100_000)
	for index := range agents {
		agents[index].AgentID = fmt.Sprintf("agent-%06d", index)
	}
	reader := &countingSQLReader{sqlReader: store.readDB}
	parents, err := store.inferAgentParents(context.Background(), reader, "codex", "evidence-free", agents)
	if err != nil {
		t.Fatal(err)
	}
	if len(parents) != 0 {
		t.Fatalf("unexpected inferred parents: %#v", parents)
	}
	if reader.queryCount != 32 {
		t.Fatalf("parent evidence queries = %d, want global maximum 32", reader.queryCount)
	}
}
