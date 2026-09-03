package sqlite

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"github.com/google/go-cmp/cmp"
	"github.com/kotokumu/agentmetry/internal/query"
	"testing"

	_ "modernc.org/sqlite"
)

func Test_traceSpanOffset(t *testing.T) {
	type args struct {
		ctx     context.Context
		reader  sqlReader
		traceID string
		spanID  string
	}
	tests := []struct {
		name    string
		args    args
		want    int
		wantErr bool
	}{
		{
			name: "native target ranks after correlated log and before equal-time log",
			args: args{ctx: context.Background(), traceID: "trace-a", spanID: "span-target", reader: func() sqlReader {
				db := must(sql.Open("sqlite", ":memory:"))
				t.Cleanup(func() { _ = db.Close() })
				for _, statement := range []string{
					"CREATE TABLE spans (trace_id TEXT, span_id TEXT, started_at TEXT, ended_at TEXT)",
					"CREATE TABLE logs (id INTEGER, trace_id TEXT, span_id TEXT, observed_at TEXT)",
					"INSERT INTO spans VALUES ('trace-a', 'span-first', '2026-09-04T01:00:00Z', '2026-09-04T01:00:01Z'), ('trace-a', 'span-target', '2026-09-04T01:00:10Z', '2026-09-04T01:00:10Z'), ('trace-b', 'span-target', '2026-09-04T00:00:00Z', '2026-09-04T00:00:00Z')",
					"INSERT INTO logs VALUES (1, 'trace-a', 'span-target', '2026-09-04T01:00:05Z'), (2, 'trace-a', 'span-target', '2026-09-04T01:00:10Z')",
				} {
					if _, err := db.Exec(statement); err != nil {
						t.Fatal(err)
					}
				}
				return db
			}()}, want: 2,
		},
		{
			name: "logs alone do not satisfy native span target",
			args: args{ctx: context.Background(), traceID: "trace-a", spanID: "span-target", reader: func() sqlReader {
				db := must(sql.Open("sqlite", ":memory:"))
				t.Cleanup(func() { _ = db.Close() })
				for _, statement := range []string{
					"CREATE TABLE spans (trace_id TEXT, span_id TEXT, started_at TEXT, ended_at TEXT)",
					"CREATE TABLE logs (id INTEGER, trace_id TEXT, span_id TEXT, observed_at TEXT)",
					"INSERT INTO logs VALUES (1, 'trace-a', 'span-target', '2026-09-04T01:00:05Z')",
				} {
					if _, err := db.Exec(statement); err != nil {
						t.Fatal(err)
					}
				}
				return db
			}()}, want: 0, wantErr: true,
		},
		{
			name: "another trace cannot satisfy native span target",
			args: args{ctx: context.Background(), traceID: "trace-a", spanID: "span-target", reader: func() sqlReader {
				db := must(sql.Open("sqlite", ":memory:"))
				t.Cleanup(func() { _ = db.Close() })
				for _, statement := range []string{
					"CREATE TABLE spans (trace_id TEXT, span_id TEXT, started_at TEXT, ended_at TEXT)",
					"CREATE TABLE logs (id INTEGER, trace_id TEXT, span_id TEXT, observed_at TEXT)",
					"INSERT INTO spans VALUES ('trace-b', 'span-target', '2026-09-04T01:00:00Z', '2026-09-04T01:00:01Z')",
				} {
					if _, err := db.Exec(statement); err != nil {
						t.Fatal(err)
					}
				}
				return db
			}()}, want: 0, wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := traceSpanOffset(tt.args.ctx, tt.args.reader, tt.args.traceID, tt.args.spanID)
			if (err != nil) != tt.wantErr {
				t.Fatalf("traceSpanOffset() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.wantErr, errors.Is(err, query.ErrTraceTargetNotFound)); diff != "" {
				t.Errorf("target-not-found error mismatch (-want +got): %s", diff)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("trace offset mismatch (-want +got): %s", diff)
			}
		})
	}
}
