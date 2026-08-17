package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestHarnessEvidenceQueryUsesSourceSessionIndex(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	rows, err := database.readDB.QueryContext(context.Background(), `EXPLAIN QUERY PLAN
SELECT otlp_exports.harness_receipt_state, COUNT(*)
FROM observations
JOIN otlp_exports ON otlp_exports.id = observations.export_id
WHERE observations.source = ?
  AND observations.session_id IN (SELECT value FROM json_each(?))
  AND observations.kind <> 'unknown'
  AND observations.signal IN ('trace', 'log')
GROUP BY otlp_exports.harness_receipt_state`, "codex", `["session-1"]`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	plan := ""
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan += detail + "\n"
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan, "observations_harness_evidence_idx") {
		t.Fatalf("query plan did not use harness evidence index:\n%s", plan)
	}
}
