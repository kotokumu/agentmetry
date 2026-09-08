package query

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	FileOutputAvailable     = "available"
	FileOutputNotReported   = "not_reported"
	FileOutputRedacted      = "redacted"
	FileOutputNotReturned   = "not_returned"
	FileOutputNotConfirmed  = "not_confirmed"
	FileMappingConfirmed    = "confirmed"
	FileMappingNotConfirmed = "not_confirmed"
	FileCoverageComplete    = "complete"
	FileCoveragePartial     = "partial"
	FileCoverageUnavailable = "unavailable"
)

type SessionFileReadFilter struct {
	Identity  ConversationIdentity
	Page      Page
	Reference string
}

type SessionFileRead struct {
	ID                 string
	SourceID           string
	SessionID          string
	Reference          string
	ActivityID         string
	ObservedAt         time.Time
	AgentID            string
	Model              string
	OutputContent      string
	OutputAvailability string
	OutputMapping      string
	ContentEvidence    ContentEvidence
}

type SessionFileReadPage struct {
	Reads                  []SessionFileRead
	DistinctReferenceCount int64
	NextOffset             int
	HasMore                bool
	Coverage               string
}

type SessionFileReadReader interface {
	ListSessionFileReads(context.Context, SessionFileReadFilter) (SessionFileReadPage, error)
}

// ReportedFileReference identifies the received attribute that reported a
// file reference. It does not represent a file on the local filesystem.
type ReportedFileReference struct {
	Value  string
	Fields []string
}

// ReportedFileReferences extracts only the structured file fields already
// admitted by the content contract. In particular, body_ref is a request-body
// reference and is intentionally excluded.
func ReportedFileReferences(activity Activity) []string {
	references := ReportedFileReferencesWithFields(activity)
	result := make([]string, 0, len(references))
	for _, reference := range references {
		result = append(result, reference.Value)
	}
	return result
}

// ReportedFileReferencesWithFields extracts only the structured file fields
// already admitted by the content contract. body_ref is a request-body
// reference and is intentionally excluded.
func ReportedFileReferencesWithFields(activity Activity) []ReportedFileReference {
	seen := make(map[string]struct{})
	indexByValue := make(map[string]int)
	var result []ReportedFileReference
	add := func(value, field string) {
		value = strings.TrimSpace(value)
		if value == "" || unavailableFileReference(value) {
			return
		}
		if _, exists := seen[value]; !exists {
			seen[value] = struct{}{}
			indexByValue[value] = len(result)
			result = append(result, ReportedFileReference{Value: value, Fields: []string{field}})
			return
		}
		index := indexByValue[value]
		for _, existing := range result[index].Fields {
			if existing == field {
				return
			}
		}
		result[index].Fields = append(result[index].Fields, field)
	}
	for _, field := range []string{"file_path", "file_paths"} {
		addFileAttribute(func(value string) { add(value, field) }, activity.Attributes[field])
	}
	for _, field := range []string{"tool_input", "tool_parameters"} {
		object := contentArguments(activity.Attributes[field])
		if object == nil {
			continue
		}
		addFileAttribute(func(value string) { add(value, field) }, object["file_path"])
		addFileAttribute(func(value string) { add(value, field) }, object["file_paths"])
	}
	return result
}

func unavailableFileReference(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "[redacted]", "<redacted>", "(redacted)", "redacted",
		"[secret]", "<secret>", "(secret)", "secret",
		"[not_returned]", "<not_returned>", "not_returned",
		"[not reported]", "<not reported>", "not reported", "not_reported":
		return true
	default:
		return false
	}
}

func addFileAttribute(add func(string), value any) {
	switch value := value.(type) {
	case string:
		add(value)
	case []string:
		for _, item := range value {
			add(item)
		}
	case []any:
		for _, item := range value {
			if text, ok := item.(string); ok {
				add(text)
			}
		}
	case nil:
		return
	default:
		// Some storage fixtures retain arrays as JSON text. Decode only this
		// admitted field shape; arbitrary attributes remain opaque.
		var values []string
		if text, ok := value.(fmt.Stringer); ok {
			_ = json.Unmarshal([]byte(text.String()), &values)
		}
		for _, item := range values {
			add(item)
		}
	}
}

func SessionFileReadID(sourceID, sessionID, activityID, reference string) string {
	key := sourceID + "\x00" + sessionID + "\x00" + activityID + "\x00" + reference
	digest := sha256.Sum256([]byte(key))
	return "file-read-" + hex.EncodeToString(digest[:12])
}
