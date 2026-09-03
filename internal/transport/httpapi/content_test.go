package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/query"
	"github.com/kotokumu/agentmetry/internal/transport/httpapi"
)

func TestContentEvidenceAcrossHTTPActivityReads(t *testing.T) {
	tests := []struct {
		name string
		url  string
		path []string
	}{
		{"overview recent", "/api/v1/overview", []string{"overview", "recentActivity"}},
		{"overview session", "/api/v1/overview", []string{"overview", "sessions", "0", "activities"}},
		{"session page", "/api/v1/session-activities?source=codex&sessionId=run", []string{"activities"}},
		{"conversation", "/api/v1/conversations/codex/run", []string{"conversation", "activities"}},
		{"trace", "/api/v1/traces/0123456789abcdef0123456789abcdef", []string{"trace", "activities"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			activities := []query.Activity{
				{ID: "redacted", Source: "codex", Signal: "log", Content: "[REDACTED]", Attributes: map[string]any{"prompt": "[REDACTED]", "private": "ATTRIBUTE_SENTINEL"}},
				{ID: "reference", Source: "claude", Signal: "log", Content: "file:///request", Attributes: map[string]any{"body_ref": "file:///request"}},
				{ID: "absent", Source: "codex", Signal: "log", Name: "gen_ai.user_prompt"},
			}
			reader := &overviewReader{value: query.Overview{RecentActivity: activities, Sessions: []query.Session{{ID: "run", SourceID: "codex", ActivityCount: 3, Activities: activities}}}, conversation: query.Session{ID: "run", SourceID: "codex", ActivityCount: 3, Activities: activities}, trace: query.Trace{ActivityCount: 3, Activities: activities}}
			response := httptest.NewRecorder()
			httpapi.New(reader, nil, time.Now).ServeHTTP(response, httptest.NewRequest(http.MethodGet, tt.url, nil))
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d: %s", response.Code, response.Body.String())
			}
			var payload any
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			for _, part := range tt.path {
				if part == "0" {
					payload = payload.([]any)[0]
				} else {
					payload = payload.(map[string]any)[part]
				}
			}
			encoded, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			var got []query.Activity
			if err := json.Unmarshal(encoded, &got); err != nil {
				t.Fatal(err)
			}
			want := []query.Activity{
				{ID: "redacted", Source: "codex", Signal: "log", ContentEvidence: &query.ContentEvidence{Source: "codex", ActivityID: "redacted", Signal: "log", Kind: "prompt", Evidence: "unknown", Availability: "redacted", Fields: []string{"prompt"}, RedactionReason: "producer_redacted"}},
				{ID: "reference", Source: "claude", Signal: "log", Content: "file:///request", ContentEvidence: &query.ContentEvidence{Source: "claude", ActivityID: "reference", Signal: "log", Kind: "reference", Evidence: "reference", Availability: "not_reported", Fields: []string{"body_ref"}}},
				{ID: "absent", Source: "codex", Signal: "log", Name: "gen_ai.user_prompt", ContentEvidence: &query.ContentEvidence{Source: "codex", ActivityID: "absent", Signal: "log", Kind: "prompt", Evidence: "unknown", Availability: "not_reported"}},
			}
			if diff := cmp.Diff(want, got, cmp.AllowUnexported(canonical.TokenUsage{})); diff != "" {
				t.Errorf("activities (-want +got): %s", diff)
			}
			if strings.Contains(response.Body.String(), "ATTRIBUTE_SENTINEL") || strings.Contains(response.Body.String(), "[REDACTED]") {
				t.Errorf("response leaks body/attrs: %s", response.Body.String())
			}
			if activities[0].Content != "[REDACTED]" || activities[0].ContentEvidence != nil {
				t.Errorf("read mutated shared query activities: %#v", activities[0])
			}
		})
	}
}
