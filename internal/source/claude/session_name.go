package claude

import (
	"encoding/json"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	source "github.com/kotokumu/agentmetry/sourceplugin"
)

// SessionName extracts Claude-generated names from title-generation responses.
func (Plugin) SessionName(event source.Event) (source.SessionName, bool) {
	nativeName, hasNativeName := event.Attributes["event.name"]
	isResponse := sourceEventName(event.Name, text(nativeName)) == "assistant_response"
	// Native OTLP EventName is normalized before storage, while event.name
	// attributes, when supplied, are retained and must take precedence.
	if !hasNativeName && event.Name == "gen_ai.response.completed" {
		isResponse = true
	}
	if !isResponse || text(event.Attributes["query_source"]) != "generate_session_title" {
		return source.SessionName{}, false
	}
	conversationID := text(event.Attributes["session.id"])
	if strings.TrimSpace(conversationID) == "" {
		return source.SessionName{}, false
	}
	if canonicalID, present := event.Attributes["gen_ai.conversation.id"]; present && canonicalID != conversationID {
		return source.SessionName{}, false
	}
	title, ok := generatedTitle(text(event.Attributes["response"]))
	if !ok {
		return source.SessionName{}, false
	}
	observedAt, _ := time.Parse(time.RFC3339Nano, text(event.Attributes["event.timestamp"]))
	if year := observedAt.UTC().Year(); year < 1 || year > 9999 {
		observedAt = time.Time{}
	}
	return source.SessionName{
		ConversationID: conversationID,
		Text:           title,
		Origin:         "claude_code.generate_session_title",
		ObservedAt:     observedAt,
	}, true
}

// generatedTitle accepts a complete object, retaining duplicate-key detection
// that unmarshalling directly into a struct or map would lose.
func generatedTitle(response string) (string, bool) {
	if !utf8.ValidString(response) {
		return "", false
	}
	decoder := json.NewDecoder(strings.NewReader(response))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return "", false
	}
	var title string
	found := false
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return "", false
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return "", false
		}
		if key == "title" {
			if found || json.Unmarshal(value, &title) != nil {
				return "", false
			}
			found = true
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return "", false
	}
	if _, err := decoder.Token(); err != io.EOF {
		return "", false
	}
	if !found || strings.TrimSpace(title) == "" {
		return "", false
	}
	for _, marker := range []string{"[truncated]", "<truncated>", "(truncated)", "<redacted>"} {
		if strings.Contains(strings.ToLower(title), marker) {
			return "", false
		}
	}
	return title, true
}
