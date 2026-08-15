package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/theoden9014/agentmetry/internal/canonical"
	"github.com/theoden9014/agentmetry/internal/query"
)

func TestProjectionFeedTenThousandCommitBurstIsReadInBoundedWindows(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	transaction, err := store.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 10_000; index++ {
		if _, err := appendProjectionChange(context.Background(), transaction, []query.ChangeTarget{query.SessionTarget("codex", "hot")}); err != nil {
			t.Fatal(err)
		}
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}

	position, err := store.CurrentProjectionPosition(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cursor := query.ProjectionPosition{Generation: position.Generation}
	windows := 0
	for cursor.Sequence < position.Sequence {
		window, err := store.ReadProjectionChanges(context.Background(), cursor, 256, 1024)
		if err != nil {
			t.Fatal(err)
		}
		if window.Through.Sequence <= cursor.Sequence || window.Through.Sequence-cursor.Sequence > 256 {
			t.Fatalf("unbounded/non-advancing window: %#v", window)
		}
		if len(window.Targets) != 1 {
			t.Fatalf("deduplicated targets = %#v", window.Targets)
		}
		cursor = window.Through
		windows++
	}
	if windows != 40 {
		t.Fatalf("windows = %d, want 40", windows)
	}
}

func TestProjectionFeedRequiresResyncBeyondRetainedHistory(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	initial, err := store.CurrentProjectionPosition(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := store.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < projectionFeedRetention+1_024; index++ {
		if _, err := appendProjectionChange(context.Background(), transaction, []query.ChangeTarget{query.OverviewTarget()}); err != nil {
			t.Fatal(err)
		}
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadProjectionChanges(context.Background(), initial, 256, 1_024); err != query.ErrProjectionCursorExpired {
		t.Fatalf("expired cursor error = %v", err)
	}
}

func BenchmarkProjectionFeedWindowRead(b *testing.B) {
	store, err := Open(filepath.Join(b.TempDir(), "agentmetry.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = store.Close() })
	transaction, err := store.db.BeginTx(context.Background(), nil)
	if err != nil {
		b.Fatal(err)
	}
	for index := 0; index < 10_000; index++ {
		if _, err := appendProjectionChange(context.Background(), transaction, []query.ChangeTarget{query.SessionTarget("codex", "hot")}); err != nil {
			b.Fatal(err)
		}
	}
	if err := transaction.Commit(); err != nil {
		b.Fatal(err)
	}
	position, _ := store.CurrentProjectionPosition(context.Background())
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		cursor := query.ProjectionPosition{Generation: position.Generation}
		for cursor.Sequence < position.Sequence {
			window, readErr := store.ReadProjectionChanges(context.Background(), cursor, 256, 1024)
			if readErr != nil {
				b.Fatal(readErr)
			}
			cursor = window.Through
		}
	}
}

func BenchmarkHotSessionCommitAfterOneHundredThousandActivities(b *testing.B) {
	store, err := Open(filepath.Join(b.TempDir(), "agentmetry.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC()
	for chunk := 0; chunk < 100; chunk++ {
		logs := make([]canonical.Log, 1000)
		for index := range logs {
			logs[index] = canonical.Log{Source: "codex", ObservedAt: now.Add(time.Duration(chunk*1000+index) * time.Nanosecond), Kind: canonical.ActivityMessage, Body: "load", Agent: canonical.AgentContext{RunID: "hot", AgentID: "main"}}
		}
		if err := store.CommitBatch(context.Background(), canonical.Batch{Logs: logs}); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		log := canonical.Log{Source: "codex", ObservedAt: now.Add(time.Duration(100_000+index) * time.Nanosecond), Kind: canonical.ActivityMessage, Body: "live", Agent: canonical.AgentContext{RunID: "hot", AgentID: "main"}}
		if err := store.CommitBatch(context.Background(), canonical.Batch{Logs: []canonical.Log{log}}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHotSessionSpanRevisionAfterOneHundredThousandActivities(b *testing.B) {
	store, err := Open(filepath.Join(b.TempDir(), "agentmetry.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC()
	for chunk := 0; chunk < 100; chunk++ {
		logs := make([]canonical.Log, 1000)
		for index := range logs {
			logs[index] = canonical.Log{Source: "codex", ObservedAt: now.Add(time.Duration(chunk*1000+index) * time.Nanosecond), Kind: canonical.ActivityMessage, Body: "load", Agent: canonical.AgentContext{RunID: "hot", AgentID: "main"}}
		}
		if err := store.CommitBatch(context.Background(), canonical.Batch{Logs: logs}); err != nil {
			b.Fatal(err)
		}
	}
	span := canonical.Span{Source: "codex", TraceID: "11111111111111111111111111111111", SpanID: "1111111111111111", Kind: canonical.ActivityResponse, StartedAt: now, EndedAt: now, Agent: canonical.AgentContext{RunID: "hot", AgentID: "main"}}
	if err := store.CommitBatch(context.Background(), canonical.Batch{Spans: []canonical.Span{span}}); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		span.EndedAt = now.Add(time.Duration(index+1) * time.Second)
		span.Agent.Tokens = canonical.TokenUsage{Input: int64(index + 1), Output: 1}
		if err := store.CommitBatch(context.Background(), canonical.Batch{Spans: []canonical.Span{span}}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHotSessionSummaryAfterOneHundredThousandActivities(b *testing.B) {
	store, err := Open(filepath.Join(b.TempDir(), "agentmetry.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC()
	for chunk := 0; chunk < 100; chunk++ {
		logs := make([]canonical.Log, 1000)
		for index := range logs {
			logs[index] = canonical.Log{Source: "codex", ObservedAt: now.Add(time.Duration(chunk*1000+index) * time.Nanosecond), Kind: canonical.ActivityMessage, Body: "load", Agent: canonical.AgentContext{RunID: "hot", AgentID: "main"}}
		}
		if err := store.CommitBatch(context.Background(), canonical.Batch{Logs: logs}); err != nil {
			b.Fatal(err)
		}
	}
	identity, err := query.NewConversationIdentity("codex", "hot")
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := store.GetSessionSummary(context.Background(), identity); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHotSessionSummaryAfterOneHundredThousandSpans(b *testing.B) {
	store, err := Open(filepath.Join(b.TempDir(), "agentmetry.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC()
	for chunk := 0; chunk < 100; chunk++ {
		spans := make([]canonical.Span, 1000)
		for index := range spans {
			ordinal := chunk*1000 + index
			spans[index] = canonical.Span{
				Source: "codex", TraceID: fmt.Sprintf("%032x", ordinal/1000+1), SpanID: fmt.Sprintf("%016x", ordinal+1),
				Kind: canonical.ActivityResponse, StartedAt: now, EndedAt: now.Add(time.Duration(ordinal) * time.Nanosecond),
				Agent: canonical.AgentContext{RunID: "hot-spans", AgentID: "main"},
			}
		}
		if err := store.CommitBatch(context.Background(), canonical.Batch{Spans: spans}); err != nil {
			b.Fatal(err)
		}
	}
	identity, err := query.NewConversationIdentity("codex", "hot-spans")
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := store.GetSessionSummary(context.Background(), identity); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHotChildAgentSummaryAfterOneHundredThousandSpans(b *testing.B) {
	store, err := Open(filepath.Join(b.TempDir(), "agentmetry.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC()
	for chunk := 0; chunk < 100; chunk++ {
		spans := make([]canonical.Span, 1000)
		for index := range spans {
			ordinal := chunk*1000 + index
			spans[index] = canonical.Span{
				Source: "codex", TraceID: fmt.Sprintf("%032x", ordinal/1000+1), SpanID: fmt.Sprintf("%016x", ordinal+1),
				ParentSpanID: fmt.Sprintf("%016x", ordinal+1_000_001), Kind: canonical.ActivityResponse,
				StartedAt: now, EndedAt: now.Add(time.Duration(ordinal) * time.Nanosecond),
				Agent: canonical.AgentContext{RunID: "hot-child-spans", AgentID: "reviewer"},
			}
		}
		if err := store.CommitBatch(context.Background(), canonical.Batch{Spans: spans}); err != nil {
			b.Fatal(err)
		}
	}
	identity, err := query.NewConversationIdentity("codex", "hot-child-spans")
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := store.GetSessionSummary(context.Background(), identity); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHotTraceHeadAfterOneHundredThousandActivities(b *testing.B) {
	store, err := Open(filepath.Join(b.TempDir(), "agentmetry.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC()
	const traceValue = "11111111111111111111111111111111"
	for chunk := 0; chunk < 100; chunk++ {
		logs := make([]canonical.Log, 1000)
		for index := range logs {
			logs[index] = canonical.Log{Source: "codex", TraceID: traceValue, ObservedAt: now.Add(time.Duration(chunk*1000+index) * time.Nanosecond), Kind: canonical.ActivityMessage, Body: "load", Agent: canonical.AgentContext{RunID: "hot", AgentID: "main"}}
		}
		if err := store.CommitBatch(context.Background(), canonical.Batch{Logs: logs}); err != nil {
			b.Fatal(err)
		}
	}
	traceID, err := query.ParseTraceID(traceValue)
	if err != nil {
		b.Fatal(err)
	}
	page, err := query.NewPage(0, 100)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := store.GetTrace(context.Background(), query.TraceFilter{TraceID: traceID, Page: page}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTenThousandSpanCommit(b *testing.B) {
	store, err := Open(filepath.Join(b.TempDir(), "agentmetry.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		spans := make([]canonical.Span, 10_000)
		for index := range spans {
			spans[index] = canonical.Span{
				Source: "codex", TraceID: fmt.Sprintf("%016x%016x", iteration, index), SpanID: fmt.Sprintf("%016x", index),
				Kind: canonical.ActivityResponse, StartedAt: now, EndedAt: now,
				Agent: canonical.AgentContext{RunID: "hot", AgentID: "main"},
			}
		}
		if err := store.CommitBatch(context.Background(), canonical.Batch{Spans: spans}); err != nil {
			b.Fatal(err)
		}
	}
}
