package connectapi

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	v1 "github.com/kotokumu/agentmetry/gen/agentmetry/v1"
	"github.com/kotokumu/agentmetry/internal/query"
	"google.golang.org/protobuf/testing/protocmp"
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
