package codex

import (
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	source "github.com/kotokumu/agentmetry/sourceplugin"
)

var listResultEnvelope = regexp.MustCompile(`\AWall time: [0-9]+(?:\.[0-9]+)? seconds\nOutput:\n`)

const listResultTruncation = "\n[... telemetry preview truncated ...]"

// SessionNames interprets received Codex app list results, without fetching names.
func (Plugin) SessionNames(event source.Event) []source.SessionName {
	name := event.Name
	if native, present := event.Attributes["event.name"]; present {
		name = stringValue(native)
		if name != "codex.tool_result" {
			return nil
		}
	}
	if (name != "codex.tool_result" && name != "gen_ai.tool_result") ||
		stringValue(event.Attributes["tool_namespace"]) != "mcp__codex_app" ||
		stringValue(event.Attributes["tool_name"]) != "list_threads" ||
		(event.Attributes["success"] != true && event.Attributes["success"] != "true") {
		return nil
	}
	output := stringValue(event.Attributes["output"])
	output = listResultEnvelope.ReplaceAllString(output, "")
	truncated := event.Attributes["output_truncated"] == true && strings.HasSuffix(output, listResultTruncation)
	if truncated {
		output = strings.TrimSuffix(output, listResultTruncation)
	}
	if !utf8.ValidString(output) {
		return nil
	}
	names, err := listedSessionNames(json.NewDecoder(strings.NewReader(output)))
	if err != nil && !(truncated && (errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF))) {
		return nil
	}
	observedAt, _ := time.Parse(time.RFC3339Nano, stringValue(event.Attributes["event.timestamp"]))
	if year := observedAt.UTC().Year(); year < 1 || year > 9999 {
		observedAt = time.Time{}
	}
	for index := range names {
		names[index].ObservedAt = observedAt
	}
	return names
}

// listedSessionNames preserves complete entries on EOF only after a supported
// schema was established. Syntax errors never permit prefix recovery.
func listedSessionNames(decoder *json.Decoder) ([]source.SessionName, error) {
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return nil, errors.New("expected list result object")
	}
	var names []source.SessionName
	seen := make(map[string]bool)
	version := false
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return namesAfterVersion(names, version), err
		}
		switch key {
		case "schemaVersion", "pinnedThreads", "threads":
			field := key.(string)
			if seen[field] {
				return nil, errors.New("duplicate list result field")
			}
			seen[field] = true
			if field == "schemaVersion" {
				var value json.RawMessage
				if err := decoder.Decode(&value); err != nil {
					return nil, err
				}
				if string(value) != "4" {
					return nil, errors.New("unsupported list result version")
				}
				version = true
			} else {
				entries, err := listedNameEntries(decoder)
				names = append(names, entries...)
				if err != nil {
					return namesAfterVersion(names, version), err
				}
			}
		default:
			var ignored json.RawMessage
			if err := decoder.Decode(&ignored); err != nil {
				return namesAfterVersion(names, version), err
			}
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return namesAfterVersion(names, version), err
	}
	if closing != json.Delim('}') || !version {
		return nil, errors.New("incomplete list result structure")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errors.New("trailing list result data")
	}
	return names, nil
}

func namesAfterVersion(names []source.SessionName, version bool) []source.SessionName {
	if !version {
		return nil
	}
	return names
}

func listedNameEntries(decoder *json.Decoder) ([]source.SessionName, error) {
	opening, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if opening != json.Delim('[') {
		return nil, errors.New("expected thread array")
	}
	var names []source.SessionName
	for decoder.More() {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return names, err
		}
		if name, ok := listedName(raw); ok {
			names = append(names, name)
		}
	}
	closing, err := decoder.Token()
	if err != nil {
		return names, err
	}
	if closing != json.Delim(']') {
		return nil, errors.New("expected thread array end")
	}
	return names, nil
}

func listedName(raw json.RawMessage) (source.SessionName, bool) {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return source.SessionName{}, false
	}
	fields := make(map[string]string)
	for decoder.More() {
		key, _ := decoder.Token() // raw is already a complete, valid JSON value.
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return source.SessionName{}, false
		}
		if key == "id" || key == "kind" || key == "title" {
			field := key.(string)
			if _, duplicate := fields[field]; duplicate {
				return source.SessionName{}, false
			}
			var text string
			if json.Unmarshal(value, &text) != nil || !completeNameValue(text) {
				return source.SessionName{}, false
			}
			fields[field] = text
		}
	}
	if fields["kind"] != "codex" || fields["id"] == "" || fields["title"] == "" {
		return source.SessionName{}, false
	}
	return source.SessionName{ConversationID: fields["id"], Text: fields["title"], Origin: "codex_app.list_threads"}, true
}

func completeNameValue(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	for _, marker := range []string{"[truncated]", "<truncated>", "(truncated)", "<redacted>", "[redacted]", "[... telemetry preview truncated ...]"} {
		if strings.Contains(strings.ToLower(value), marker) {
			return false
		}
	}
	return true
}
