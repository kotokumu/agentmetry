package claude

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	source "github.com/kotokumu/agentmetry/sourceplugin"
)

func TestPlugin_SessionName(t *testing.T) {
	// This is an anonymized received-event shape, not a UI-title ground truth.
	fixtureBytes, err := os.ReadFile("testdata/session_name_observed.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture source.Event
	if err := json.Unmarshal(fixtureBytes, &fixture); err != nil {
		t.Fatal(err)
	}
	type args struct {
		event source.Event
	}
	tests := []struct {
		name  string
		p     Plugin
		args  args
		want  source.SessionName
		want1 bool
	}{
		{
			name:  "anonymized observed response",
			args:  args{event: fixture},
			want:  source.SessionName{ConversationID: "anonymous-conversation-001", Text: "セッション一覧を改善", Origin: "claude_code.generate_session_title", ObservedAt: time.Date(2026, 9, 6, 3, 4, 5, 123456789, time.UTC)},
			want1: true,
		},
		{
			name:  "native event without attribute name",
			args:  args{event: source.Event{Name: "claude_code.assistant_response", Attributes: map[string]any{"query_source": "generate_session_title", "session.id": "conversation", "response": `{"title":"Name"}`}}},
			want:  source.SessionName{ConversationID: "conversation", Text: "Name", Origin: "claude_code.generate_session_title"},
			want1: true,
		},
		{
			name:  "native prefixed attribute name",
			args:  args{event: source.Event{Name: "gen_ai.response.completed", Attributes: map[string]any{"event.name": "claude_code.assistant_response", "query_source": "generate_session_title", "session.id": "conversation", "response": `{"title":"Name"}`, "event.timestamp": "invalid"}}},
			want:  source.SessionName{ConversationID: "conversation", Text: "Name", Origin: "claude_code.generate_session_title"},
			want1: true,
		},
		{
			name:  "preserve text and ignore unrelated JSON properties",
			args:  args{event: source.Event{Name: "assistant_response", Attributes: map[string]any{"query_source": "generate_session_title", "session.id": "conversation", "response": `{"title":"  名前 <script>  ","other":{"nested":true}}`}}},
			want:  source.SessionName{ConversationID: "conversation", Text: "  名前 <script>  ", Origin: "claude_code.generate_session_title"},
			want1: true,
		},
		{
			name:  "normal response cannot name conversation",
			args:  args{event: source.Event{Name: "assistant_response", Attributes: map[string]any{"query_source": "repl_main_thread", "session.id": "conversation", "response": `{"title":"Name"}`}}},
			want:  source.SessionName{},
			want1: false,
		},
		{
			name:  "API request is not a generated response",
			args:  args{event: source.Event{Name: "api_request", Attributes: map[string]any{"query_source": "generate_session_title", "session.id": "conversation", "response": `{"title":"Name"}`}}},
			want:  source.SessionName{},
			want1: false,
		},
		{
			name:  "stored canonical event retains generated response evidence",
			args:  args{event: source.Event{Name: "gen_ai.response.completed", Attributes: map[string]any{"query_source": "generate_session_title", "session.id": "conversation", "response": `{"title":"Name"}`}}},
			want:  source.SessionName{ConversationID: "conversation", Text: "Name", Origin: "claude_code.generate_session_title"},
			want1: true,
		},
		{
			name:  "canonical name cannot override explicit conflicting native event",
			args:  args{event: source.Event{Name: "gen_ai.response.completed", Attributes: map[string]any{"event.name": "api_request", "query_source": "generate_session_title", "session.id": "conversation", "response": `{"title":"Name"}`}}},
			want:  source.SessionName{},
			want1: false,
		},
		{
			name:  "explicit empty native event does not permit canonical fallback",
			args:  args{event: source.Event{Name: "gen_ai.response.completed", Attributes: map[string]any{"event.name": "", "query_source": "generate_session_title", "session.id": "conversation", "response": `{"title":"Name"}`}}},
			want:  source.SessionName{},
			want1: false,
		},
		{
			name:  "year zero timestamp is unknown",
			args:  args{event: source.Event{Name: "assistant_response", Attributes: map[string]any{"query_source": "generate_session_title", "session.id": "conversation", "response": `{"title":"Name"}`, "event.timestamp": "0000-01-01T00:00:00Z"}}},
			want:  source.SessionName{ConversationID: "conversation", Text: "Name", Origin: "claude_code.generate_session_title"},
			want1: true,
		},
		{
			name:  "timestamp offset outside minimum supported year is unknown",
			args:  args{event: source.Event{Name: "assistant_response", Attributes: map[string]any{"query_source": "generate_session_title", "session.id": "conversation", "response": `{"title":"Name"}`, "event.timestamp": "0001-01-01T00:00:00+01:00"}}},
			want:  source.SessionName{ConversationID: "conversation", Text: "Name", Origin: "claude_code.generate_session_title"},
			want1: true,
		},
		{
			name:  "timestamp offset outside maximum supported year is unknown",
			args:  args{event: source.Event{Name: "assistant_response", Attributes: map[string]any{"query_source": "generate_session_title", "session.id": "conversation", "response": `{"title":"Name"}`, "event.timestamp": "9999-12-31T23:59:59-01:00"}}},
			want:  source.SessionName{ConversationID: "conversation", Text: "Name", Origin: "claude_code.generate_session_title"},
			want1: true,
		},
		{
			name:  "missing native conversation ID",
			args:  args{event: source.Event{Name: "assistant_response", Attributes: map[string]any{"query_source": "generate_session_title", "gen_ai.conversation.id": "conversation", "response": `{"title":"Name"}`}}},
			want:  source.SessionName{},
			want1: false,
		},
		{
			name:  "blank native conversation ID",
			args:  args{event: source.Event{Name: "assistant_response", Attributes: map[string]any{"query_source": "generate_session_title", "session.id": " ", "response": `{"title":"Name"}`}}},
			want:  source.SessionName{},
			want1: false,
		},
		{
			name:  "conflicting canonical ID",
			args:  args{event: source.Event{Name: "assistant_response", Attributes: map[string]any{"query_source": "generate_session_title", "session.id": "conversation", "gen_ai.conversation.id": "different", "response": `{"title":"Name"}`}}},
			want:  source.SessionName{},
			want1: false,
		},
		{
			name:  "redacted response",
			args:  args{event: source.Event{Name: "assistant_response", Attributes: map[string]any{"query_source": "generate_session_title", "session.id": "conversation", "response": "<REDACTED>"}}},
			want:  source.SessionName{},
			want1: false,
		},
		{
			name:  "truncated JSON",
			args:  args{event: source.Event{Name: "assistant_response", Attributes: map[string]any{"query_source": "generate_session_title", "session.id": "conversation", "response": `{"title":"Name`}}},
			want:  source.SessionName{},
			want1: false,
		},
		{
			name:  "truncation marker in valid JSON",
			args:  args{event: source.Event{Name: "assistant_response", Attributes: map[string]any{"query_source": "generate_session_title", "session.id": "conversation", "response": `{"title":"Name [TRUNCATED]"}`}}},
			want:  source.SessionName{},
			want1: false,
		},
		{
			name:  "duplicate title keys",
			args:  args{event: source.Event{Name: "assistant_response", Attributes: map[string]any{"query_source": "generate_session_title", "session.id": "conversation", "response": `{"title":"One","title":"Two"}`}}},
			want:  source.SessionName{},
			want1: false,
		},
		{
			name:  "escaped duplicate title keys",
			args:  args{event: source.Event{Name: "assistant_response", Attributes: map[string]any{"query_source": "generate_session_title", "session.id": "conversation", "response": `{"title":"One","\u0074itle":"Two"}`}}},
			want:  source.SessionName{},
			want1: false,
		},
		{
			name:  "non-string title",
			args:  args{event: source.Event{Name: "assistant_response", Attributes: map[string]any{"query_source": "generate_session_title", "session.id": "conversation", "response": `{"title":123}`}}},
			want:  source.SessionName{},
			want1: false,
		},
		{
			name:  "null title",
			args:  args{event: source.Event{Name: "assistant_response", Attributes: map[string]any{"query_source": "generate_session_title", "session.id": "conversation", "response": `{"title":null}`}}},
			want:  source.SessionName{},
			want1: false,
		},
		{
			name:  "whitespace title",
			args:  args{event: source.Event{Name: "assistant_response", Attributes: map[string]any{"query_source": "generate_session_title", "session.id": "conversation", "response": `{"title":" \n\t"}`}}},
			want:  source.SessionName{},
			want1: false,
		},
		{
			name:  "missing title",
			args:  args{event: source.Event{Name: "assistant_response", Attributes: map[string]any{"query_source": "generate_session_title", "session.id": "conversation", "response": `{"summary":"Name"}`}}},
			want:  source.SessionName{},
			want1: false,
		},
		{
			name:  "object response attribute rejected",
			args:  args{event: source.Event{Name: "assistant_response", Attributes: map[string]any{"query_source": "generate_session_title", "session.id": "conversation", "response": map[string]any{"title": "Name"}}}},
			want:  source.SessionName{},
			want1: false,
		},
		{
			name:  "top-level array",
			args:  args{event: source.Event{Name: "assistant_response", Attributes: map[string]any{"query_source": "generate_session_title", "session.id": "conversation", "response": `[{"title":"Name"}]`}}},
			want:  source.SessionName{},
			want1: false,
		},
		{
			name:  "trailing JSON value",
			args:  args{event: source.Event{Name: "assistant_response", Attributes: map[string]any{"query_source": "generate_session_title", "session.id": "conversation", "response": `{"title":"Name"} {}`}}},
			want:  source.SessionName{},
			want1: false,
		},
		{
			name:  "content alias is not native response",
			args:  args{event: source.Event{Name: "assistant_response", Attributes: map[string]any{"query_source": "generate_session_title", "session.id": "conversation", "content": `{"title":"Name"}`}}},
			want:  source.SessionName{},
			want1: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Plugin{}
			got, got1 := p.SessionName(tt.args.event)
			if !cmp.Equal(tt.want, got) {
				t.Errorf("Plugin.SessionName() got = %v, want %v\ndiff=%s", got, tt.want, cmp.Diff(tt.want, got))
			}
			if got1 != tt.want1 {
				t.Errorf("Plugin.SessionName() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}
