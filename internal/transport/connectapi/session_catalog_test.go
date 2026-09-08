package connectapi

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	v1 "github.com/kotokumu/agentmetry/gen/agentmetry/v1"
	"github.com/kotokumu/agentmetry/internal/query"
	"github.com/kotokumu/agentmetry/internal/source/claude"
	"github.com/kotokumu/agentmetry/sourceplugin"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestSessionListViewContract(t *testing.T) {
	for _, tt := range []struct {
		name    string
		view    v1.SessionListView
		want    query.SessionListView
		wantErr bool
	}{
		{name: "legacy default", want: query.SessionListRoots},
		{name: "roots", view: v1.SessionListView_SESSION_LIST_VIEW_ROOTS, want: query.SessionListRoots},
		{name: "all", view: v1.SessionListView_SESSION_LIST_VIEW_ALL, want: query.SessionListAll},
		{name: "unknown", view: 99, wantErr: true},
		{name: "negative", view: -1, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reader := &readerStub{sessions: query.SessionPage{AppliedView: tt.want}}
			server := &Server{reader: reader, now: time.Now}
			response, err := server.ListSessions(context.Background(), connect.NewRequest(&v1.ListSessionsRequest{View: tt.view}))
			if (err != nil) != tt.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tt.wantErr)
			}
			if tt.wantErr {
				if connect.CodeOf(err) != connect.CodeInvalidArgument {
					t.Fatal(err)
				}
				return
			}
			if diff := cmp.Diff(tt.want, reader.lastSessions.View); diff != "" {
				t.Fatal(diff)
			}
			want := v1.SessionListView_SESSION_LIST_VIEW_ROOTS
			if tt.want == query.SessionListAll {
				want = v1.SessionListView_SESSION_LIST_VIEW_ALL
			}
			if diff := cmp.Diff(want, response.Msg.AppliedView); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestSessionNameWireContract(t *testing.T) {
	for _, tt := range []struct {
		name   string
		source string
		input  *query.SessionName
		want   *v1.SessionName
	}{
		{name: "codex observed name with time", source: "codex", input: &query.SessionName{Text: "Observed", Origin: "codex_app.list_threads", ObservedAt: time.Unix(1788652800, 0)}, want: &v1.SessionName{Text: "Observed", Origin: "codex_app.list_threads", ObservedAt: timestamppb.New(time.Unix(1788652800, 0))}},
		{name: "codex observed name unknown time", source: "codex", input: &query.SessionName{Text: "Observed", Origin: "codex_app.list_threads"}, want: &v1.SessionName{Text: "Observed", Origin: "codex_app.list_threads"}},
		{name: "absent stays absent"},
		{name: "generated name with unknown time", source: "claude", input: &query.SessionName{Text: "改善する", Origin: "claude_code.generate_session_title"}, want: &v1.SessionName{Text: "改善する", Origin: "claude_code.generate_session_title"}},
		{name: "generated name with observation time", source: "claude", input: &query.SessionName{Text: "Improve sessions", Origin: "claude_code.generate_session_title", ObservedAt: time.Unix(1788652800, 0)}, want: &v1.SessionName{Text: "Improve sessions", Origin: "claude_code.generate_session_title", ObservedAt: timestamppb.New(time.Unix(1788652800, 0))}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := mapSessions([]query.SessionListEntry{{Session: query.Session{ID: "session", SourceID: tt.source}, RootSessionID: "session", Name: tt.input}})
			if diff := cmp.Diff(tt.want, got[0].Catalog.Name, protocmp.Transform()); diff != "" {
				t.Fatal(diff)
			}
			if _, err := protojson.Marshal(&v1.ListSessionsResponse{Sessions: got}); err != nil {
				t.Fatalf("serialize session list: %v", err)
			}
		})
	}
}

func TestSessionNameTimestampJSON(t *testing.T) {
	for _, tt := range []struct {
		name      string
		timestamp string
		want      *v1.SessionName
	}{
		{name: "UTC exceeds upper bound", timestamp: "9999-12-31T23:59:59-01:00", want: &v1.SessionName{Text: "Name", Origin: "claude_code.generate_session_title"}},
		{name: "UTC exceeds lower bound", timestamp: "0001-01-01T00:00:00+01:00", want: &v1.SessionName{Text: "Name", Origin: "claude_code.generate_session_title"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			observation, ok := claude.New().SessionName(sourceplugin.Event{Name: "assistant_response", Attributes: map[string]any{
				"query_source": "generate_session_title", "session.id": "conversation", "response": `{"title":"Name"}`, "event.timestamp": tt.timestamp,
			}})
			if !ok {
				t.Fatal("valid name observation was lost")
			}
			got := mapSessions([]query.SessionListEntry{{Session: query.Session{ID: "conversation", SourceID: "claude"}, Name: &query.SessionName{Text: observation.Text, Origin: observation.Origin, ObservedAt: observation.ObservedAt}}})
			encoded, err := protojson.Marshal(&v1.ListSessionsResponse{Sessions: got})
			if err != nil {
				t.Fatal(err)
			}
			var decoded v1.ListSessionsResponse
			if err := protojson.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.want, decoded.Sessions[0].Catalog.Name, protocmp.Transform()); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestSessionCatalogIsListOnly(t *testing.T) {
	reader := &readerStub{sessions: query.SessionPage{AppliedView: query.SessionListAll, Sessions: []query.SessionListEntry{
		{Session: query.Session{ID: "child", SourceID: "codex"}, RootSessionID: "root", ParentSessionID: "parent"},
	}}, conversation: query.Session{ID: "root", SourceID: "codex"}}
	server := &Server{reader: reader, now: time.Now}
	page, err := server.ListSessions(context.Background(), connect.NewRequest(&v1.ListSessionsRequest{View: v1.SessionListView_SESSION_LIST_VIEW_ALL}))
	if err != nil {
		t.Fatal(err)
	}
	want := &v1.SessionCatalog{Role: v1.SessionRole_SESSION_ROLE_CHILD, RootSessionId: "root", ParentSessionId: "parent"}
	if diff := cmp.Diff(want, page.Msg.Sessions[0].Catalog, protocmp.Transform()); diff != "" {
		t.Fatal(diff)
	}
	detail, err := server.GetSession(context.Background(), connect.NewRequest(&v1.GetSessionRequest{SourceId: "codex", SessionId: "child"}))
	if err != nil {
		t.Fatal(err)
	}
	if detail.Msg.Session.Catalog != nil || detail.Msg.Session.Id != "root" {
		t.Fatalf("detail changed: %v", detail.Msg.Session)
	}
}
