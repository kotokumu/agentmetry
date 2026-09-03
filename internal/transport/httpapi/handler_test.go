package httpapi_test

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/kotokumu/agentmetry/internal/planusage"
	"github.com/kotokumu/agentmetry/internal/query"
	"github.com/kotokumu/agentmetry/internal/transport/httpapi"
)

type planImporter struct {
	source   string
	raw      []byte
	captured time.Time
}

func (importer *planImporter) ImportRaw(_ context.Context, source string, raw []byte, captured time.Time) ([]planusage.Snapshot, error) {
	importer.source = source
	importer.raw = append([]byte(nil), raw...)
	importer.captured = captured
	return []planusage.Snapshot{{Source: source, WindowID: "weekly"}}, nil
}

type overviewReader struct {
	filter             query.OverviewFilter
	value              query.Overview
	traceFilter        query.TraceFilter
	trace              query.Trace
	traceErr           error
	conversationFilter query.ConversationFilter
	conversation       query.Session
	conversationErr    error
}

func (reader *overviewReader) GetOverview(_ context.Context, filter query.OverviewFilter) (query.Overview, error) {
	reader.filter = filter
	return reader.value, nil
}

func (reader *overviewReader) GetTrace(_ context.Context, filter query.TraceFilter) (query.Trace, error) {
	reader.traceFilter = filter
	return reader.trace, reader.traceErr
}

func (reader *overviewReader) GetConversation(_ context.Context, filter query.ConversationFilter) (query.Session, error) {
	reader.conversationFilter = filter
	return reader.conversation, reader.conversationErr
}

func TestOverviewAPIUsesRangeAndReturnsVersionedJSON(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	reader := &overviewReader{value: query.Overview{SignalCounts: query.SignalCounts{Traces: 2}}}
	handler := httpapi.New(reader, testAssets(), func() time.Time { return now })
	request := httptest.NewRequest(http.MethodGet, "/api/v1/overview?range=1h", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if want := now.Add(-time.Hour); !reader.filter.Since.Equal(want) {
		t.Fatalf("since = %s, want %s", reader.filter.Since, want)
	}
	if reader.filter.ActivityOffset != 0 || reader.filter.ActivityLimit != 100 {
		t.Fatalf("activity page = offset %d limit %d, want 0/100", reader.filter.ActivityOffset, reader.filter.ActivityLimit)
	}
	var payload struct {
		Version  string         `json:"version"`
		Overview query.Overview `json:"overview"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Version != "v1" || payload.Overview.SignalCounts.Traces != 2 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestOverviewAPIPassesOptionalSessionFilters(t *testing.T) {
	reader := &overviewReader{}
	handler := httpapi.New(reader, testAssets(), time.Now)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/overview?source=claude&q=repository+review", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if reader.filter.SourceID != "claude" || reader.filter.Search != "repository review" {
		t.Fatalf("unexpected session filter: %#v", reader.filter)
	}
}

func TestSessionActivitiesAPIRequestsTheNextBoundedPage(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	reader := &overviewReader{value: query.Overview{Sessions: []query.Session{{
		ID:            "session-1",
		SourceID:      "example",
		ActivityCount: 205,
		Activities:    []query.Activity{{Name: "activity-101"}},
	}}}}
	handler := httpapi.New(reader, testAssets(), func() time.Time { return now })
	request := httptest.NewRequest(http.MethodGet, "/api/v1/session-activities?source=example&sessionId=session-1&range=7d&offset=100&limit=100", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if reader.filter.ActivityOffset != 100 || reader.filter.ActivityLimit != 100 {
		t.Fatalf("activity page = offset %d limit %d, want 100/100", reader.filter.ActivityOffset, reader.filter.ActivityLimit)
	}
	var payload struct {
		Activities []query.Activity `json:"activities"`
		Total      int64            `json:"total"`
		HasMore    bool             `json:"hasMore"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Activities) != 1 || payload.Total != 205 || !payload.HasMore {
		t.Fatalf("unexpected page: %#v", payload)
	}
}

func TestSessionActivitiesAPIRejectsUnboundedPages(t *testing.T) {
	handler := httpapi.New(&overviewReader{}, testAssets(), time.Now)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/session-activities?source=example&sessionId=session-1&offset=0&limit=1000", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}

func TestConversationAPILoadsAnExactSourceQualifiedConversation(t *testing.T) {
	const traceID = "11111111111111111111111111111111"
	const spanID = "aaaaaaaaaaaaaaaa"
	reader := &overviewReader{conversation: query.Session{
		ID: "conversation/1", SourceID: "example source", ActivityCount: 101,
		Activities: []query.Activity{{TraceID: traceID, SpanID: spanID}},
	}}
	handler := httpapi.New(reader, testAssets(), time.Now)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/example%20source/conversation%2F1?traceId="+traceID+"&spanId="+spanID+"&offset=25&limit=50&mode=page", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	identity, err := query.NewConversationIdentity("example source", "conversation/1")
	if err != nil {
		t.Fatal(err)
	}
	anchor, err := query.NewActivityAnchor(traceID, spanID)
	if err != nil {
		t.Fatal(err)
	}
	page, err := query.NewPage(25, 50)
	if err != nil {
		t.Fatal(err)
	}
	if reader.conversationFilter != (query.ConversationFilter{Identity: identity, Anchor: anchor, Page: page, PageMode: query.ConversationPageFromOffset}) {
		t.Fatalf("unexpected conversation filter: %#v", reader.conversationFilter)
	}
	if !strings.Contains(response.Body.String(), `"spanId":"`+spanID+`"`) {
		t.Fatalf("target span missing from response: %s", response.Body.String())
	}
}

func TestConversationAPIRequiresTraceAndSpanTogether(t *testing.T) {
	handler := httpapi.New(&overviewReader{}, testAssets(), time.Now)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/example/conversation-1?spanId=aaaaaaaaaaaaaaaa", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "provided together") {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
}

func TestConversationAPIMapsUnknownIdentityToNotFound(t *testing.T) {
	handler := httpapi.New(&overviewReader{conversationErr: query.ErrConversationNotFound}, testAssets(), time.Now)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/example/missing", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "conversation not found") {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
}

func TestTraceAPIReturnsCompleteVersionedTrace(t *testing.T) {
	const traceID = "11111111111111111111111111111111"
	reader := &overviewReader{trace: query.Trace{TraceID: traceID, RootSpanCount: 1}}
	handler := httpapi.New(reader, testAssets(), time.Now)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/traces/"+traceID, nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if reader.traceFilter.TraceID.String() != traceID {
		t.Fatalf("unexpected trace filter: %#v", reader.traceFilter)
	}
	var payload struct {
		Version string      `json:"version"`
		Trace   query.Trace `json:"trace"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Version != "v1" || payload.Trace.TraceID != traceID || payload.Trace.RootSpanCount != 1 {
		t.Fatalf("unexpected trace payload: %#v", payload)
	}
	if strings.Contains(response.Body.String(), `"conversations":null`) || strings.Contains(response.Body.String(), `"agents":null`) || strings.Contains(response.Body.String(), `"activities":null`) {
		t.Fatalf("trace collections must serialize as arrays: %s", response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/v1/traces/"+traceID+"?spanId=ABCDEFABCDEFABCD&limit=3&offset=110", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || reader.traceFilter.SpanID.String() != "abcdefabcdefabcd" || reader.traceFilter.Page.Size() != 3 || reader.traceFilter.Page.Offset() != 110 {
		t.Errorf("anchor read mapping: %d, %#v", response.Code, reader.traceFilter)
	}
	for _, suffix := range []string{"?spanId=not-a-span", "?spanId=0000000000000000", "?spanId=ABCDEFABCDEFABCD&limit=101"} {
		reader.traceFilter = query.TraceFilter{}
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/traces/"+traceID+suffix, nil))
		if response.Code != http.StatusBadRequest || reader.traceFilter.TraceID.String() != "" {
			t.Errorf("invalid anchor request %q reached reader or wrong status: %d", suffix, response.Code)
		}
	}
}

func TestTraceAPIMapsUnknownTraceToNotFound(t *testing.T) {
	reader := &overviewReader{traceErr: query.ErrTraceNotFound}
	handler := httpapi.New(reader, testAssets(), time.Now)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/traces/22222222222222222222222222222222", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "trace not found") {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}

	reader.traceErr = query.ErrTraceTargetNotFound
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/traces/22222222222222222222222222222222?spanId=abcdefabcdefabcd", nil))
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "target span not found") {
		t.Errorf("native target not-found mapping: %d %s", response.Code, response.Body.String())
	}
}

func TestSessionActivitiesAPIRequiresSourceQualifiedIdentity(t *testing.T) {
	handler := httpapi.New(&overviewReader{}, testAssets(), time.Now)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/session-activities?sessionId=session-1", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "source is required") {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
}

func TestTraceAPIRejectsInvalidIdentity(t *testing.T) {
	handler := httpapi.New(&overviewReader{}, testAssets(), time.Now)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/traces/%20%20", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}

func TestUnknownBrowserRouteFallsBackToSPA(t *testing.T) {
	handler := httpapi.New(&overviewReader{}, testAssets(), time.Now)
	request := httptest.NewRequest(http.MethodGet, "/sessions/session-1", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Body.String() != "<main>agentmetry</main>" {
		t.Fatalf("unexpected SPA response: %d %q", response.Code, response.Body.String())
	}
}

func TestTraceBrowserRouteLoadsTheEmbeddedSPA(t *testing.T) {
	handler := httpapi.New(&overviewReader{}, testAssets(), time.Now)
	request := httptest.NewRequest(http.MethodGet, "/traces/11111111111111111111111111111111", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Body.String() != "<main>agentmetry</main>" {
		t.Fatalf("unexpected trace SPA response: %d %q", response.Code, response.Body.String())
	}
}

func TestConversationBrowserRouteLoadsTheEmbeddedSPA(t *testing.T) {
	handler := httpapi.New(&overviewReader{}, testAssets(), time.Now)
	request := httptest.NewRequest(http.MethodGet, "/conversations/example/conversation-1?spanId=span-1", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Body.String() != "<main>agentmetry</main>" {
		t.Fatalf("unexpected conversation SPA response: %d %q", response.Code, response.Body.String())
	}
}

func TestPlanUsageAPIStoresAuthoritativeSnapshot(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	importer := &planImporter{}
	handler := httpapi.New(&overviewReader{}, testAssets(), func() time.Time { return now }, importer)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/plan-usage", strings.NewReader(`{
  "source":"example","raw":{"rateLimits":{"usedPercent":25}}
}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("response=%d body=%s", response.Code, response.Body.String())
	}
	if importer.source != "example" || !importer.captured.Equal(now) || string(importer.raw) != `{"rateLimits":{"usedPercent":25}}` {
		t.Fatalf("import = source:%q captured:%s raw:%s", importer.source, importer.captured, importer.raw)
	}
}

func testAssets() fs.FS {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<main>agentmetry</main>")},
	}
}
