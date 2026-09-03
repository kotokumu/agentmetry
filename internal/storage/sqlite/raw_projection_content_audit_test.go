package sqlite

import (
	"context"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace/ptraceotlp"

	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/ingest"
	adapter "github.com/kotokumu/agentmetry/internal/ingest/otel"
	"github.com/kotokumu/agentmetry/internal/journal"
	"github.com/kotokumu/agentmetry/internal/query"
	"github.com/kotokumu/agentmetry/internal/source/codex"
	source "github.com/kotokumu/agentmetry/sourceplugin"
)

func TestValidOTLPJournalRetainsRawSpanEventBeyondProjection(t *testing.T) {
	now := time.Date(2026, 9, 4, 1, 2, 3, 0, time.UTC)
	request := ptraceotlp.NewExportRequest()
	resource := request.Traces().ResourceSpans().AppendEmpty()
	resource.Resource().Attributes().PutStr("service.name", "codex")
	scope := resource.ScopeSpans().AppendEmpty()
	scope.Scope().SetName("codex.fixture")
	span := scope.Spans().AppendEmpty()
	traceID := pcommon.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	spanID := pcommon.SpanID{17, 18, 19, 20, 21, 22, 23, 24}
	span.SetTraceID(traceID)
	span.SetSpanID(spanID)
	span.SetName("codex.tool_result")
	span.SetStartTimestamp(pcommon.NewTimestampFromTime(now))
	span.SetEndTimestamp(pcommon.NewTimestampFromTime(now.Add(2 * time.Second)))
	span.Attributes().PutStr("conversation.id", "run-fixture")
	span.Attributes().PutStr("tool_name", "exec_command")
	span.Attributes().PutStr("arguments", `{"cmd":"cat AGENTS.md"}`)
	span.Attributes().PutStr("output", "projected output")
	span.Attributes().PutInt("input_tokens", 10)
	span.Attributes().PutInt("output_tokens", 2)
	event := span.Events().AppendEmpty()
	event.SetName("codex.tool_result")
	event.SetTimestamp(pcommon.NewTimestampFromTime(now.Add(time.Second)))
	event.Attributes().PutStr("output", "RAW_EVENT_OUTPUT_SENTINEL")
	event.Attributes().PutStr("call_id", "call-7")
	event.Attributes().PutInt("input_tokens", 999)

	payload, err := request.MarshalProto()
	if err != nil {
		t.Fatal(err)
	}
	normalizer := adapter.NewNormalizer(source.NewRegistry(codex.New()))
	projection, err := normalizer.NormalizeTraces(request.Traces())
	if err != nil {
		t.Fatal(err)
	}
	observations, err := adapter.BuildTraceObservations(request.Traces(), projection)
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalTrace, ingest.TransportGRPC, now, payload), Projection: projection, Observations: observations}
	accepted.Journal = ingest.DeriveJournalMetadata(observations, projection, "")
	if err := store.CommitExport(context.Background(), accepted); err != nil {
		t.Fatal(err)
	}

	var compressed []byte
	var codec, hashText string
	var size int
	if err := store.db.QueryRow("SELECT payload_protobuf,payload_codec,payload_size,payload_sha256 FROM otlp_exports").Scan(&compressed, &codec, &size, &hashText); err != nil {
		t.Fatal(err)
	}
	hashBytes, err := hex.DecodeString(hashText)
	if err != nil {
		t.Fatal(err)
	}
	var hash [32]byte
	copy(hash[:], hashBytes)
	raw, err := journal.Restore(journal.Codec(codec), compressed, size, hash)
	if err != nil {
		t.Fatal(err)
	}
	restored := ptraceotlp.NewExportRequest()
	if err := restored.UnmarshalProto(raw); err != nil {
		t.Fatal(err)
	}
	rawSpan := restored.Traces().ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0)
	rawEvent := rawSpan.Events().At(0)
	if rawSpan.TraceID() != traceID || rawSpan.SpanID() != spanID || rawSpan.StartTimestamp().AsTime() != now || rawSpan.EndTimestamp().AsTime() != now.Add(2*time.Second) {
		t.Fatalf("raw identity/time changed: %#v", rawSpan)
	}
	if rawEvent.Name() != "codex.tool_result" || rawEvent.Timestamp().AsTime() != now.Add(time.Second) {
		t.Fatalf("raw event identity/time changed: %#v", rawEvent)
	}
	if value, ok := rawEvent.Attributes().Get("output"); !ok || value.Str() != "RAW_EVENT_OUTPUT_SENTINEL" {
		t.Fatalf("raw event output missing: %v %v", value, ok)
	}
	if value, ok := rawEvent.Attributes().Get("input_tokens"); !ok || value.Int() != 999 {
		t.Fatalf("raw event usage missing: %v %v", value, ok)
	}

	parsedTraceID, err := query.ParseTraceID(traceID.String())
	if err != nil {
		t.Fatal(err)
	}
	trace, err := store.GetTrace(context.Background(), query.TraceFilter{TraceID: parsedTraceID})
	if err != nil {
		t.Fatal(err)
	}
	if len(trace.Activities) != 1 {
		t.Fatalf("projection count=%d, want one span and no event activity", len(trace.Activities))
	}
	projected := trace.Activities[0]
	if projected.TraceID != traceID.String() || projected.SpanID != spanID.String() || !projected.StartedAt.Equal(now) || !projected.EndedAt.Equal(now.Add(2*time.Second)) {
		t.Fatalf("projected identity/time changed: %#v", projected)
	}
	if projected.Content != "Result: projected output" || projected.Tokens.Input != 10 || projected.Tokens.Output != 2 {
		t.Fatalf("projected content/usage: %#v", projected)
	}
	if projected.Content == "RAW_EVENT_OUTPUT_SENTINEL" || projected.Tokens.Input == 999 {
		t.Fatalf("raw event was independently projected or double counted: %#v", projected)
	}
}

func TestOTLPTraceCountsUnknownKindAcrossDetailOverviewAndWindow(t *testing.T) {
	now := time.Date(2026, 9, 4, 2, 0, 0, 0, time.UTC)
	request := ptraceotlp.NewExportRequest()
	resource := request.Traces().ResourceSpans().AppendEmpty()
	resource.Resource().Attributes().PutStr("service.name", "codex")
	spans := resource.ScopeSpans().AppendEmpty().Spans()
	traceID := pcommon.TraceID{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x09, 0x99}
	for index, name := range []string{"gen_ai.user_prompt", "custom.received.context", "gen_ai.response.completed"} {
		span := spans.AppendEmpty()
		span.SetTraceID(traceID)
		span.SetSpanID(pcommon.SpanID{0, 0, 0, 0, 0, 0, 0, byte(index + 1)})
		span.SetName(name)
		span.SetStartTimestamp(pcommon.NewTimestampFromTime(now.Add(time.Duration(index) * time.Second)))
		span.SetEndTimestamp(pcommon.NewTimestampFromTime(now.Add(time.Duration(index+1) * time.Second)))
		span.Attributes().PutStr("conversation.id", "unknown-kind-run")
		span.Attributes().PutStr("content", fmt.Sprintf("received-%d", index))
	}
	projection, err := adapter.NewNormalizer(source.NewRegistry(codex.New())).NormalizeTraces(request.Traces())
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.Spans) != 3 || projection.Spans[1].Kind != canonical.ActivityUnknown {
		t.Fatalf("OTLP fixture did not retain one unknown-kind activity: %#v", projection.Spans)
	}
	payload, err := request.MarshalProto()
	if err != nil {
		t.Fatal(err)
	}
	observations, err := adapter.BuildTraceObservations(request.Traces(), projection)
	if err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(t.TempDir(), "agentmetry.db")
	database, err := Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	accepted := ingest.AcceptedExport{Envelope: ingest.NewEnvelope(canonical.SignalTrace, ingest.TransportGRPC, now, payload), Projection: projection, Observations: observations}
	accepted.Journal = ingest.DeriveJournalMetadata(observations, projection, "")
	if err := database.CommitExport(context.Background(), accepted); err != nil {
		t.Fatal(err)
	}
	parsedTraceID, err := query.ParseTraceID(traceID.String())
	if err != nil {
		t.Fatal(err)
	}
	page := must(query.NewPage(0, 100))
	detail, err := database.GetTrace(context.Background(), query.TraceFilter{TraceID: parsedTraceID, Page: page})
	if err != nil {
		t.Fatal(err)
	}
	overview, err := database.GetTraceOverview(context.Background(), parsedTraceID)
	if err != nil {
		t.Fatal(err)
	}
	window, err := database.GetTraceWindow(context.Background(), query.TraceWindowFilter{TraceID: parsedTraceID, Window: query.TraceWindow{Kind: canonical.ActivityUnknown}, Page: page})
	if err != nil {
		t.Fatal(err)
	}
	if detail.ActivityCount != 3 || len(detail.Activities) != 3 {
		t.Fatalf("GetTrace returned %d activities with summary count %d", len(detail.Activities), detail.ActivityCount)
	}
	if overview.TotalActivities != 3 || overview.ReturnedActivities != 3 || len(overview.Activities) != 3 {
		t.Fatalf("overview count mismatch: %#v", overview)
	}
	if window.Trace.ActivityCount != 3 || window.MatchingActivities != 1 || len(window.Trace.Activities) != 1 || window.Trace.Activities[0].Kind != canonical.ActivityUnknown {
		t.Fatalf("unknown-kind window mismatch: %#v", window)
	}
	if _, err := database.db.ExecContext(context.Background(), "UPDATE trace_rollups SET activity_count = 2, root_span_count = 2 WHERE trace_id = ?", traceID.String()); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	database, err = Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	repaired, err := database.GetTrace(context.Background(), query.TraceFilter{TraceID: parsedTraceID, Page: page})
	if err != nil {
		t.Fatal(err)
	}
	if repaired.ActivityCount != 3 || repaired.RootSpanCount != 3 || len(repaired.Activities) != 3 {
		t.Fatalf("opening an existing projection did not repair the trace count: %#v", repaired)
	}
}
