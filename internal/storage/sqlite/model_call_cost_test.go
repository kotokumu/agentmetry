package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/kotokumu/agentmetry/internal/billing"
	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/ingest"
	"github.com/kotokumu/agentmetry/internal/ingest/otel"
	"github.com/kotokumu/agentmetry/internal/observation"
	"github.com/kotokumu/agentmetry/internal/query"
	"github.com/kotokumu/agentmetry/internal/source/builtin"
	claudesource "github.com/kotokumu/agentmetry/internal/source/claude"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
)

func TestOpenSeedsTemporalOpenAIRates(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM model_rates`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Fatalf("model rate count = %d, want 5", count)
	}
}

func TestOpenReplayCandidateDoesNotSeedCurrentRates(t *testing.T) {
	store, err := OpenReplayCandidate(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	var count int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM model_rates`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("replay candidate model rate count = %d, want 0", count)
	}
}

func TestProviderAmountMicroUSDBoundaries(t *testing.T) {
	tests := []struct {
		name  string
		attrs map[string]any
		want  *int64
		state string
	}{
		{name: "below half", attrs: map[string]any{"gen_ai.usage.cost_usd": "0.000000499999"}, want: int64Pointer(0), state: "valid"},
		{name: "half up", attrs: map[string]any{"gen_ai.usage.cost_usd": "0.0000005"}, want: int64Pointer(1), state: "valid"},
		{name: "scientific", attrs: map[string]any{"gen_ai.usage.cost_usd": "5e-7"}, want: int64Pointer(1), state: "valid"},
		{name: "max", attrs: map[string]any{"gen_ai.usage.cost_usd": "9223372036854.775807"}, want: int64Pointer(9_223_372_036_854_775_807), state: "valid"},
		{name: "overflow", attrs: map[string]any{"gen_ai.usage.cost_usd": "9223372036854.775808"}, state: "invalid"},
		{name: "fraction syntax", attrs: map[string]any{"gen_ai.usage.cost_usd": "1/2"}, state: "invalid"},
		{name: "invalid micros blocks decimal fallback", attrs: map[string]any{"cost_usd_micros": "bad", "gen_ai.usage.cost_usd": "1"}, state: "invalid"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := claudesource.ResolveProviderCost(test.attrs)
			if string(got.State) != test.state || !equalInt64Pointer(got.AmountMicroUSD, test.want) {
				t.Fatalf("ResolveProviderCost() = %v, %q", got.AmountMicroUSD, got.State)
			}
		})
	}
}

func int64Pointer(value int64) *int64 { return &value }
func equalInt64Pointer(left, right *int64) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func TestApplyRateManifestClosesPriorIntervalAndRepricesBoundaryForward(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	boundary := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	usage := canonical.TokenUsage{Input: 100, Output: 50, CacheRead: 20, CacheWrite: 10, Presence: canonical.TokenPresence{Input: true, Output: true, CacheRead: true, CacheWrite: true}}
	for _, at := range []time.Time{boundary.Add(-time.Nanosecond), boundary, boundary.Add(time.Nanosecond)} {
		if err := store.CommitExport(ctx, costExport(at, "codex", "codex.sse_event", "gen_ai.response.completed", "rate-session", "gpt-6-astra", usage, map[string]any{"gen_ai.usage.role": "authoritative_call"})); err != nil {
			t.Fatal(err)
		}
	}
	var beforeSequence int64
	if err := store.db.QueryRow(`SELECT COALESCE(MAX(sequence), 0) FROM projection_changes`).Scan(&beforeSequence); err != nil {
		t.Fatal(err)
	}
	limit := int64(272_000)
	newRate := billing.Rate{
		Provider: "openai", Model: "gpt-6-astra", Mode: "standard", MaxInputTokensInclusive: &limit,
		EffectiveFrom: boundary,
		Pricing:       billing.Pricing{InputMicroUSDPerMillion: 20_000_000, CacheReadMicroUSDPerMillion: 2_000_000, CacheWriteMicroUSDPerMillion: 25_000_000, OutputMicroUSDPerMillion: 100_000_000},
		SourceURL:     "https://developers.openai.com/api/docs/models/gpt-6-astra", EvidenceID: "TEST-RATE", RetrievedAt: boundary.Add(time.Hour),
	}
	if err := store.ApplyRateManifest(ctx, []billing.Rate{newRate}, boundary.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var closedAt string
	if err := store.db.QueryRow(`SELECT effective_to FROM model_rates WHERE rate_id = ?`, billing.BuiltinOpenAIRates()[0].ID()).Scan(&closedAt); err != nil {
		t.Fatal(err)
	}
	if closedAt != formatTime(boundary) {
		t.Fatalf("prior effective_to = %q", closedAt)
	}
	rows, err := store.db.Query(`SELECT a.amount_micro_usd FROM model_call_attributions a JOIN model_calls c USING(call_id) WHERE c.native_session_id = 'rate-session' ORDER BY c.occurred_at`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var amounts []int64
	for rows.Next() {
		var amount int64
		if err := rows.Scan(&amount); err != nil {
			t.Fatal(err)
		}
		amounts = append(amounts, amount)
	}
	if len(amounts) != 3 || amounts[0] != 3_345 || amounts[1] != 6_690 || amounts[2] != 6_690 {
		t.Fatalf("repriced amounts = %v", amounts)
	}
	var published int64
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM activity_changes WHERE sequence > ?`, beforeSequence).Scan(&published); err != nil {
		t.Fatal(err)
	}
	if published != 4 {
		t.Fatalf("repricing activity mutations = %d, want 4 session/trace upserts", published)
	}
}

func TestApplyRateManifestPreservesMissingOccurrenceReason(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	boundary := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	usage := canonical.TokenUsage{Presence: canonical.TokenPresence{Input: true, Output: true, CacheRead: true, CacheWrite: true}}
	exported := costExport(boundary.Add(-time.Hour), "codex", "codex.sse_event", "gen_ai.response.completed", "missing-time", "gpt-6-astra", usage, map[string]any{"gen_ai.usage.role": "authoritative_call"})
	exported.Observations[0].OccurredAt = time.Time{}
	if err := store.CommitExport(ctx, exported); err != nil {
		t.Fatal(err)
	}
	limit := int64(272_000)
	rate := billing.Rate{
		Provider: "openai", Model: "gpt-6-astra", Mode: "standard", MaxInputTokensInclusive: &limit,
		EffectiveFrom: boundary,
		Pricing:       billing.Pricing{InputMicroUSDPerMillion: 20_000_000, CacheReadMicroUSDPerMillion: 2_000_000, CacheWriteMicroUSDPerMillion: 25_000_000, OutputMicroUSDPerMillion: 100_000_000},
		SourceURL:     "https://developers.openai.com/api/docs/models/gpt-6-astra", EvidenceID: "TEST-MISSING-TIME", RetrievedAt: boundary.Add(time.Hour),
	}
	if err := store.ApplyRateManifest(ctx, []billing.Rate{rate}, boundary.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var occurredAt, reason string
	if err := store.db.QueryRow(`SELECT c.occurred_at, a.primary_reason FROM model_calls c JOIN model_call_attributions a USING(call_id) WHERE c.native_session_id = 'missing-time'`).Scan(&occurredAt, &reason); err != nil {
		t.Fatal(err)
	}
	if occurredAt != "" || reason != "missing_occurred_at" {
		t.Fatalf("missing occurrence after repricing = occurred_at %q, reason %q", occurredAt, reason)
	}
}

func TestApplyRateManifestRejectionLeavesCatalogAndProjectionFeedUnchanged(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	var ratesBefore, sequenceBefore int64
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM model_rates`).Scan(&ratesBefore); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COALESCE(MAX(sequence), 0) FROM projection_changes`).Scan(&sequenceBefore); err != nil {
		t.Fatal(err)
	}
	invalid := billing.BuiltinOpenAIRates()[0]
	invalid.EffectiveFrom = invalid.EffectiveFrom.Add(-time.Hour)
	invalid.EvidenceID = "HISTORY-REWRITE"
	if err := store.ApplyRateManifest(ctx, []billing.Rate{invalid}, time.Now().UTC()); err == nil {
		t.Fatal("historical rewrite was accepted")
	}
	var ratesAfter, sequenceAfter int64
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM model_rates`).Scan(&ratesAfter); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COALESCE(MAX(sequence), 0) FROM projection_changes`).Scan(&sequenceAfter); err != nil {
		t.Fatal(err)
	}
	if ratesAfter != ratesBefore || sequenceAfter != sequenceBefore {
		t.Fatalf("rejected manifest mutated state: rates %d→%d sequence %d→%d", ratesBefore, ratesAfter, sequenceBefore, sequenceAfter)
	}
}

func TestApplyRateManifestAppendsMultipleIntervalsInOneUpdate(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	makeRate := func(month time.Month, price int64) billing.Rate {
		effective := time.Date(2026, month, 1, 0, 0, 0, 0, time.UTC)
		return billing.Rate{
			Provider: "test", Model: "model", Mode: "standard", EffectiveFrom: effective,
			Pricing: billing.Pricing{InputMicroUSDPerMillion: price}, SourceURL: "https://example.test/pricing",
			EvidenceID: "TEST-CHAIN", RetrievedAt: effective,
		}
	}
	base, middle, latest := makeRate(time.January, 1), makeRate(time.February, 2), makeRate(time.March, 3)
	if err := store.ApplyRateManifest(ctx, []billing.Rate{base}, time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyRateManifest(ctx, []billing.Rate{latest, middle}, time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	rows, err := store.db.Query(`SELECT effective_to FROM model_rates WHERE provider = 'test' ORDER BY effective_from`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []sql.NullString
	for rows.Next() {
		var value sql.NullString
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		got = append(got, value)
	}
	if len(got) != 3 || !got[0].Valid || got[0].String != formatTime(middle.EffectiveFrom) || !got[1].Valid || got[1].String != formatTime(latest.EffectiveFrom) || got[2].Valid {
		t.Fatalf("rate interval chain = %#v", got)
	}
}

func TestSessionCostSummaryReportsPartialCoverageWithoutLegacyTotal(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	at := time.Date(2026, 9, 9, 1, 0, 0, 0, time.UTC)
	usage := canonical.TokenUsage{Input: 10, Output: 2, Presence: canonical.TokenPresence{Input: true, Output: true, CacheRead: true, CacheWrite: true}}
	for _, model := range []string{"gpt-6-astra", "unknown-model"} {
		if err := store.CommitExport(ctx, costExport(at, "codex", "codex.sse_event", "gen_ai.response.completed", "partial-session", model, usage, map[string]any{"gen_ai.usage.role": "authoritative_call"})); err != nil {
			t.Fatal(err)
		}
	}
	session, err := store.GetSessionSummary(ctx, mustConversationIdentityInternal(t, "codex", "partial-session"))
	if err != nil {
		t.Fatal(err)
	}
	if session.CostSummary.Coverage != "partial" || session.CostSummary.EligibleCalls != 2 || session.CostSummary.PricedCalls != 1 || session.CostUSD != nil {
		t.Fatalf("partial summary = %#v legacy=%v", session.CostSummary, session.CostUSD)
	}
	if len(session.CostSummary.UnpricedReasons) != 1 || session.CostSummary.UnpricedReasons[0].Reason != "rate_not_found" {
		t.Fatalf("partial reasons = %#v", session.CostSummary.UnpricedReasons)
	}
}

func TestDashboardCostSelectsWholeActiveRootGroup(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	since := time.Date(2026, 9, 9, 1, 0, 0, 0, time.UTC)
	usage := canonical.TokenUsage{Input: 100, Output: 50, CacheRead: 20, CacheWrite: 10, Presence: canonical.TokenPresence{Input: true, Output: true, CacheRead: true, CacheWrite: true}}
	for _, item := range []struct {
		at      time.Time
		session string
	}{{since.Add(-time.Hour), "root"}, {since.Add(time.Hour), "child"}} {
		if err := store.CommitExport(ctx, costExport(item.at, "codex", "codex.sse_event", "gen_ai.response.completed", item.session, "gpt-6-astra", usage, map[string]any{"gen_ai.usage.role": "authoritative_call"})); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.CommitBatch(ctx, canonical.Batch{SessionLinks: []canonical.SessionLink{{Source: "codex", ParentSessionID: "root", ChildSessionID: "child", ObservedAt: since}}}); err != nil {
		t.Fatal(err)
	}
	dashboard, err := store.GetDashboard(ctx, query.DashboardFilter{Since: since})
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.CostSummary.AmountMicroUSD == nil || *dashboard.CostSummary.AmountMicroUSD != 6_690 || dashboard.CostSummary.EligibleCalls != 2 {
		t.Fatalf("dashboard root-group cost = %#v", dashboard.CostSummary)
	}
}

func TestClaudeDuplicateConflictBecomesUnavailableInsteadOfDoubleCounting(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	at := time.Date(2026, 9, 9, 1, 0, 0, 0, time.UTC)
	for _, amount := range []int64{10, 11} {
		exported := costExport(at, "claude", "api_request", "gen_ai.model.request", "conflict-session", "claude-model", canonical.TokenUsage{}, map[string]any{
			"gen_ai.usage.role": "authoritative_call", "gen_ai.client.request.id": "same-request", "cost_usd_micros": amount,
		})
		if err := store.CommitExport(ctx, exported); err != nil {
			t.Fatal(err)
		}
	}
	session, err := store.GetSessionSummary(ctx, mustConversationIdentityInternal(t, "claude", "conflict-session"))
	if err != nil {
		t.Fatal(err)
	}
	if session.CostSummary.EligibleCalls != 1 || session.CostSummary.PricedCalls != 0 || session.CostSummary.UnpricedReasons[0].Reason != "conflicting_authoritative_evidence" {
		t.Fatalf("conflicting duplicate summary = %#v", session.CostSummary)
	}
}

func TestClaudeLateAliasBridgeRekeysToOneCall(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	at := time.Date(2026, 9, 9, 1, 0, 0, 0, time.UTC)
	for _, aliases := range []map[string]any{
		{"gen_ai.client.request.id": "client-1"},
		{"gen_ai.request.id": "request-1"},
		{"gen_ai.client.request.id": "client-1", "gen_ai.request.id": "request-1"},
	} {
		aliases["gen_ai.usage.role"] = "authoritative_call"
		aliases["cost_usd_micros"] = int64(25)
		if err := store.CommitExport(ctx, costExport(at, "claude", "api_request", "gen_ai.model.request", "bridge-session", "claude-model", canonical.TokenUsage{}, aliases)); err != nil {
			t.Fatal(err)
		}
	}
	var calls, evidence, priced int64
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM model_calls WHERE source = 'claude'`).Scan(&calls); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM model_call_evidence WHERE source = 'claude'`).Scan(&evidence); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT amount_micro_usd FROM model_call_attributions WHERE source = 'claude'`).Scan(&priced); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || evidence != 3 || priced != 25 {
		t.Fatalf("bridge projection = calls %d evidence %d amount %d", calls, evidence, priced)
	}
}

func TestClaudeCorroboratingTraceSupportIsRetractedWhenAmbiguous(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	at := time.Date(2026, 9, 9, 1, 0, 0, 0, time.UTC)
	first := map[string]any{"gen_ai.usage.role": "authoritative_call", "gen_ai.client.request.id": "client-1", "gen_ai.usage.id": "shared-usage", "gen_ai.usage.id.basis": "opaque", "cost_usd_micros": int64(10)}
	if err := store.CommitExport(ctx, costExport(at, "claude", "api_request", "gen_ai.model.request", "support-session", "claude-model", canonical.TokenUsage{}, first)); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitExport(ctx, claudeCorroboratingSpanExport(at, "support-session", "shared-usage")); err != nil {
		t.Fatal(err)
	}
	const corroboratingTrace = "11112222333344445555666677778888"
	var before int64
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM model_call_trace_memberships WHERE trace_id = ?`, corroboratingTrace).Scan(&before); err != nil {
		t.Fatal(err)
	}
	second := map[string]any{"gen_ai.usage.role": "authoritative_call", "gen_ai.client.request.id": "client-2", "gen_ai.usage.id": "shared-usage", "gen_ai.usage.id.basis": "opaque", "cost_usd_micros": int64(20)}
	if err := store.CommitExport(ctx, costExport(at, "claude", "api_request", "gen_ai.model.request", "support-session", "claude-model", canonical.TokenUsage{}, second)); err != nil {
		t.Fatal(err)
	}
	var after, corroboratingLinks, directSupports int64
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM model_call_trace_memberships WHERE trace_id = ?`, corroboratingTrace).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM model_call_activity_links WHERE evidence_role = 'corroborating'`).Scan(&corroboratingLinks); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM model_call_trace_supports WHERE support_kind = 'direct'`).Scan(&directSupports); err != nil {
		t.Fatal(err)
	}
	if before != 1 || after != 0 || corroboratingLinks != 0 || directSupports != 2 {
		t.Fatalf("support lifecycle = before %d after %d corroborating %d direct %d", before, after, corroboratingLinks, directSupports)
	}
}

func TestClaudeCorroborationDoesNotCollapseDifferentAliasTypes(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	at := time.Date(2026, 9, 9, 1, 0, 0, 0, time.UTC)
	request := map[string]any{"gen_ai.usage.role": "authoritative_call", "gen_ai.request.id": "same-text", "cost_usd_micros": int64(10)}
	if err := store.CommitExport(ctx, costExport(at, "claude", "api_request", "gen_ai.model.request", "typed-session", "claude-model", canonical.TokenUsage{}, request)); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitExport(ctx, claudeCorroboratingSpanWithAlias(at, "typed-session", "gen_ai.client.request.id", "same-text")); err != nil {
		t.Fatal(err)
	}
	var links int64
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM model_call_activity_links WHERE evidence_role = 'corroborating'`).Scan(&links); err != nil {
		t.Fatal(err)
	}
	if links != 0 {
		t.Fatalf("different typed aliases produced %d corroborating links", links)
	}
}

func TestClaudeCorroboratingSpanMoveRetractsOldSessionSupport(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	at := time.Date(2026, 9, 9, 1, 0, 0, 0, time.UTC)
	request := map[string]any{"gen_ai.usage.role": "authoritative_call", "gen_ai.client.request.id": "client-1", "cost_usd_micros": int64(10)}
	if err := store.CommitExport(ctx, costExport(at, "claude", "api_request", "gen_ai.model.request", "session-a", "claude-model", canonical.TokenUsage{}, request)); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitExport(ctx, claudeCorroboratingSpanWithAlias(at, "session-a", "gen_ai.client.request.id", "client-1")); err != nil {
		t.Fatal(err)
	}
	const corroboratingTrace = "11112222333344445555666677778888"
	var before int64
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM model_call_trace_memberships WHERE trace_id = ?`, corroboratingTrace).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitExport(ctx, claudeCorroboratingSpanWithAlias(at.Add(time.Second), "session-b", "gen_ai.client.request.id", "client-1")); err != nil {
		t.Fatal(err)
	}
	var after, refs int64
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM model_call_trace_memberships WHERE trace_id = ?`, corroboratingTrace).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM model_call_activity_links WHERE evidence_role = 'corroborating'`).Scan(&refs); err != nil {
		t.Fatal(err)
	}
	if before != 1 || after != 0 || refs != 0 {
		t.Fatalf("moved span support = before %d after %d refs %d", before, after, refs)
	}
}

func TestCodexCorroboratingTraceSupportIsRetractedWhenUsageIDAmbiguous(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	at := time.Date(2026, 9, 9, 1, 0, 0, 0, time.UTC)
	usage := canonical.TokenUsage{Input: 1, Output: 1, Presence: canonical.TokenPresence{CacheRead: true, CacheWrite: true}}
	attributes := map[string]any{"gen_ai.usage.role": "authoritative_call", "gen_ai.usage.id": "usage-1"}
	call := costExport(at, "codex", "codex.sse_event", "gen_ai.response.completed", "codex-support", "gpt-6-astra", usage, attributes)
	if err := store.CommitExport(ctx, call); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitExport(ctx, codexCorroboratingSpanExport(at, "codex-support", "usage-1")); err != nil {
		t.Fatal(err)
	}
	const corroboratingTrace = "22223333444455556666777788889999"
	var before int64
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM model_call_trace_memberships WHERE trace_id = ?`, corroboratingTrace).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitExport(ctx, call); err != nil {
		t.Fatal(err)
	}
	var after, refs int64
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM model_call_trace_memberships WHERE trace_id = ?`, corroboratingTrace).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM model_call_activity_links WHERE evidence_role = 'corroborating' AND call_id IN (SELECT call_id FROM model_calls WHERE source = 'codex')`).Scan(&refs); err != nil {
		t.Fatal(err)
	}
	if before != 1 || after != 0 || refs != 0 {
		t.Fatalf("Codex support lifecycle = before %d after %d refs %d", before, after, refs)
	}
}

func TestCodexCorroboratingSpanMoveRetractsOldSessionSupport(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	at := time.Date(2026, 9, 9, 1, 0, 0, 0, time.UTC)
	usage := canonical.TokenUsage{Input: 1, Output: 1, Presence: canonical.TokenPresence{CacheRead: true, CacheWrite: true}}
	attributes := map[string]any{"gen_ai.usage.role": "authoritative_call", "gen_ai.usage.id": "usage-1"}
	if err := store.CommitExport(ctx, costExport(at, "codex", "codex.sse_event", "gen_ai.response.completed", "session-a", "gpt-6-astra", usage, attributes)); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitExport(ctx, codexCorroboratingSpanExport(at, "session-a", "usage-1")); err != nil {
		t.Fatal(err)
	}
	const corroboratingTrace = "22223333444455556666777788889999"
	var before int64
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM model_call_trace_memberships WHERE trace_id = ?`, corroboratingTrace).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitExport(ctx, codexCorroboratingSpanExport(at.Add(time.Second), "session-b", "usage-1")); err != nil {
		t.Fatal(err)
	}
	var after, refs int64
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM model_call_trace_memberships WHERE trace_id = ?`, corroboratingTrace).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM model_call_activity_links WHERE evidence_role = 'corroborating' AND call_id IN (SELECT call_id FROM model_calls WHERE source = 'codex')`).Scan(&refs); err != nil {
		t.Fatal(err)
	}
	if before != 1 || after != 0 || refs != 0 {
		t.Fatalf("moved Codex span support = before %d after %d refs %d", before, after, refs)
	}
}

func TestCommitExportAttributesClaudeAndCodexCalls(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agentmetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	at := time.Date(2026, 9, 9, 1, 0, 0, 0, time.UTC)

	claude := costExport(at, "claude", "api_request", "gen_ai.model.request", "claude-session", "claude-model", canonical.TokenUsage{}, map[string]any{
		"gen_ai.usage.role": "authoritative_call", "gen_ai.client.request.id": "request-1", "cost_usd_micros": int64(12_500),
	})
	if err := store.CommitExport(ctx, claude); err != nil {
		t.Fatal(err)
	}
	usage := canonical.TokenUsage{Input: 100, Output: 50, CacheRead: 20, CacheWrite: 10, Reasoning: 30, Presence: canonical.TokenPresence{Input: true, Output: true, CacheRead: true, CacheWrite: true, Reasoning: true}}
	codex := costExport(at, "codex", "codex.sse_event", "gen_ai.response.completed", "codex-session", "gpt-6-astra", usage, map[string]any{
		"gen_ai.usage.role": "authoritative_call", "service_tier": "default",
	})
	if err := store.CommitExport(ctx, codex); err != nil {
		t.Fatal(err)
	}

	rows, err := store.db.Query(`SELECT source, basis, amount_micro_usd, rate_entry_id FROM model_call_attributions ORDER BY source`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type result struct {
		source, basis string
		amount        int64
		rateID        sql.NullString
	}
	var got []result
	for rows.Next() {
		var item result
		if err := rows.Scan(&item.source, &item.basis, &item.amount, &item.rateID); err != nil {
			t.Fatal(err)
		}
		got = append(got, item)
	}
	if len(got) != 2 || got[0].source != "claude" || got[0].basis != "provider_reported" || got[0].amount != 12_500 || got[0].rateID.Valid {
		t.Fatalf("Claude attribution = %#v", got)
	}
	if got[1].source != "codex" || got[1].basis != "rate_card_estimate" || got[1].amount != 3_345 || !got[1].rateID.Valid || got[1].rateID.String == "" {
		t.Fatalf("Codex attribution = %#v", got[1])
	}

	// Codex fallback identity is per journal arrival, even for identical payloads.
	if err := store.CommitExport(ctx, codex); err != nil {
		t.Fatal(err)
	}
	var codexCalls int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM model_calls WHERE source = 'codex'`).Scan(&codexCalls); err != nil {
		t.Fatal(err)
	}
	if codexCalls != 2 {
		t.Fatalf("Codex calls = %d, want 2", codexCalls)
	}

	session, err := store.GetSessionSummary(ctx, mustConversationIdentityInternal(t, "codex", "codex-session"))
	if err != nil {
		t.Fatal(err)
	}
	if session.CostSummary.AmountMicroUSD == nil || *session.CostSummary.AmountMicroUSD != 6_690 || session.CostSummary.EligibleCalls != 2 || session.CostSummary.PricedCalls != 2 {
		t.Fatalf("session cost summary = %#v", session.CostSummary)
	}
	if session.CostUSD == nil || *session.CostUSD != 0.00669 {
		t.Fatalf("legacy complete session cost = %v", session.CostUSD)
	}
	dashboard, err := store.GetDashboard(ctx, query.DashboardFilter{Since: at.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.CostSummary.AmountMicroUSD == nil || *dashboard.CostSummary.AmountMicroUSD != 19_190 || dashboard.CostSummary.Basis != "mixed" {
		t.Fatalf("dashboard cost summary = %#v", dashboard.CostSummary)
	}
	traceID, err := query.ParseTraceID("00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := query.NewPage(0, 100)
	trace, err := store.GetTrace(ctx, query.TraceFilter{TraceID: traceID, Page: page})
	if err != nil {
		t.Fatal(err)
	}
	if trace.CostSummary.AmountMicroUSD == nil || *trace.CostSummary.AmountMicroUSD != 19_190 || trace.CostSummary.EligibleCalls != 3 {
		t.Fatalf("trace cost summary = %#v", trace.CostSummary)
	}
	pricedActivities := 0
	for _, activity := range trace.Activities {
		if activity.ModelCallCost != nil {
			pricedActivities++
		}
	}
	if pricedActivities != 3 {
		t.Fatalf("priced representative activities = %d, want 3", pricedActivities)
	}
}

func TestRawOTLPNormalizationProducesProviderAndUnpricedCallCosts(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agentmetry.db"), builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	at := time.Date(2026, 9, 9, 1, 0, 0, 0, time.UTC)
	claude := rawLogExport(t, at, "claude-code", "api_request", map[string]any{
		"gen_ai.conversation.id": "raw-claude", "model": "claude-model",
		"client_request_id": "request-1", "cost_usd_micros": int64(125),
	})
	codex := rawLogExport(t, at, "codex", "codex.sse_event", map[string]any{
		"event.kind": "response.completed", "conversation.id": "raw-codex", "model": "gpt-6-astra",
	})
	if err := store.CommitExport(ctx, claude); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitExport(ctx, codex); err != nil {
		t.Fatal(err)
	}
	claudeSession, err := store.GetSessionSummary(ctx, mustConversationIdentityInternal(t, "claude", "raw-claude"))
	if err != nil {
		t.Fatal(err)
	}
	if claudeSession.CostSummary.AmountMicroUSD == nil || *claudeSession.CostSummary.AmountMicroUSD != 125 || claudeSession.CostSummary.Coverage != "complete" {
		t.Fatalf("raw Claude cost = %#v", claudeSession.CostSummary)
	}
	codexSession, err := store.GetSessionSummary(ctx, mustConversationIdentityInternal(t, "codex", "raw-codex"))
	if err != nil {
		t.Fatal(err)
	}
	if codexSession.CostSummary.EligibleCalls != 1 || codexSession.CostSummary.PricedCalls != 0 || len(codexSession.CostSummary.UnpricedReasons) != 1 || codexSession.CostSummary.UnpricedReasons[0].Reason != "missing_token_usage" {
		t.Fatalf("raw usage-free Codex cost = %#v", codexSession.CostSummary)
	}
}

func mustConversationIdentityInternal(t *testing.T, source, session string) query.ConversationIdentity {
	t.Helper()
	identity, err := query.NewConversationIdentity(source, session)
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func costExport(at time.Time, source, sourceEvent, canonicalName, session, model string, usage canonical.TokenUsage, attributes map[string]any) ingest.AcceptedExport {
	const traceID = "00112233445566778899aabbccddeeff"
	return ingest.AcceptedExport{
		Envelope:     ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, at.Add(time.Second), []byte{0x0a, 0x00}),
		Observations: []observation.Observation{{Ordinal: 0, Signal: canonical.SignalLog, Source: source, SourceEventName: sourceEvent, OccurredAt: at, ObservedAt: at, TraceID: traceID, SessionID: session, Model: model, Usage: usage, NormalizerVersion: 3}},
		Projection:   canonical.Batch{Signal: canonical.SignalLog, Logs: []canonical.Log{{Source: source, ObservedAt: at, Name: canonicalName, TraceID: traceID, Kind: canonical.ActivityResponse, Attributes: attributes, Agent: canonical.AgentContext{RunID: session, Model: model, Tokens: usage}}}},
	}
}

func claudeCorroboratingSpanExport(at time.Time, session, usageID string) ingest.AcceptedExport {
	return claudeCorroboratingSpanWithAlias(at, session, "", usageID)
}

func claudeCorroboratingSpanWithAlias(at time.Time, session, aliasKey, aliasValue string) ingest.AcceptedExport {
	const traceID = "11112222333344445555666677778888"
	const spanID = "1111222233334444"
	attributes := map[string]any{"gen_ai.usage.role": "corroborating", "gen_ai.usage.id": aliasValue}
	if aliasKey != "" {
		attributes[aliasKey] = aliasValue
	}
	if aliasKey == "gen_ai.client.request.id" {
		attributes["gen_ai.usage.id.basis"] = "claude_client_request_id"
	} else if aliasKey == "" {
		attributes["gen_ai.usage.id.basis"] = "opaque"
	}
	return ingest.AcceptedExport{
		Envelope:     ingest.NewEnvelope(canonical.SignalTrace, ingest.TransportGRPC, at.Add(time.Second), []byte{0x0a, 0x01}),
		Observations: []observation.Observation{{Ordinal: 0, Signal: canonical.SignalTrace, Source: "claude", SourceEventName: "claude_code.api_request", OccurredAt: at, ObservedAt: at, TraceID: traceID, SpanID: spanID, SessionID: session, NormalizerVersion: 3}},
		Projection:   canonical.Batch{Signal: canonical.SignalTrace, Spans: []canonical.Span{{Source: "claude", TraceID: traceID, SpanID: spanID, Name: "claude request", StartedAt: at, EndedAt: at.Add(time.Millisecond), Kind: canonical.ActivityResponse, Attributes: attributes, Agent: canonical.AgentContext{RunID: session}}}},
	}
}

func codexCorroboratingSpanExport(at time.Time, session, usageID string) ingest.AcceptedExport {
	const traceID = "22223333444455556666777788889999"
	const spanID = "2222333344445555"
	attributes := map[string]any{"gen_ai.usage.role": "corroborating", "gen_ai.usage.id": usageID}
	return ingest.AcceptedExport{
		Envelope:     ingest.NewEnvelope(canonical.SignalTrace, ingest.TransportGRPC, at.Add(time.Second), []byte{0x0a, 0x02}),
		Observations: []observation.Observation{{Ordinal: 0, Signal: canonical.SignalTrace, Source: "codex", SourceEventName: "codex.sse_event", OccurredAt: at, ObservedAt: at, TraceID: traceID, SpanID: spanID, SessionID: session, NormalizerVersion: 3}},
		Projection:   canonical.Batch{Signal: canonical.SignalTrace, Spans: []canonical.Span{{Source: "codex", TraceID: traceID, SpanID: spanID, Name: "codex response", StartedAt: at, EndedAt: at.Add(time.Millisecond), Kind: canonical.ActivityResponse, Attributes: attributes, Agent: canonical.AgentContext{RunID: session}}}},
	}
}

func rawLogExport(t *testing.T, at time.Time, service, eventName string, attributes map[string]any) ingest.AcceptedExport {
	t.Helper()
	logs := plog.NewLogs()
	resource := logs.ResourceLogs().AppendEmpty()
	resource.Resource().Attributes().PutStr("service.name", service)
	record := resource.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	record.SetEventName(eventName)
	record.SetTimestamp(pcommon.NewTimestampFromTime(at))
	record.SetObservedTimestamp(pcommon.NewTimestampFromTime(at.Add(time.Second)))
	if err := record.Attributes().FromRaw(attributes); err != nil {
		t.Fatal(err)
	}
	payload, err := plogotlp.NewExportRequestFromLogs(logs).MarshalProto()
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := otel.ReplayExport(canonical.SignalLog, ingest.TransportGRPC, at.Add(2*time.Second), payload, builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	return accepted
}
