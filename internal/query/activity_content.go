package query

import (
	"encoding/json"
	"strings"
)

// ContentEvidence describes received projected content without carrying body,
// reference values, ciphertext, or arbitrary producer attributes.
type ContentEvidence struct {
	Source          string   `json:"source"`
	ActivityID      string   `json:"activityId"`
	Signal          string   `json:"signal"`
	Kind            string   `json:"kind"`
	Evidence        string   `json:"evidence"`
	Availability    string   `json:"availability"`
	Fields          []string `json:"fields,omitempty"`
	Truncated       bool     `json:"truncated"`
	RedactionReason string   `json:"redactionReason,omitempty"`
}

// DescribeActivityContent interprets only verified existing projection fields.
// A body without enough received provenance remains semantically unknown.
func DescribeActivityContent(activity Activity) ContentEvidence {
	evidence := ContentEvidence{
		Source: activity.Source, ActivityID: activity.ID, Signal: string(activity.Signal),
		Kind: "unknown", Evidence: "unknown", Availability: "not_reported",
	}
	if activity.Content != "" {
		evidence.Availability = "available"
	}
	switch activity.Source {
	case "claude":
		return describeClaudeContent(activity, evidence)
	case "codex":
		return describeCodexContent(activity, evidence)
	default:
		return evidence
	}
}

func describeClaudeContent(activity Activity, evidence ContentEvidence) ContentEvidence {
	name := contentEventName(activity)
	if activity.Content == "" {
		if name == "assistant_response" || name == "response.completed" {
			evidence.Kind = "response"
		} else if name == "user_prompt" {
			evidence.Kind = "prompt"
		}
		return evidence
	}
	// These are the existing Claude content aliases. Matching the projected
	// string prevents unrelated/native content from inheriting an attribute's meaning.
	for _, field := range []string{"prompt", "response", "tool_input", "tool_parameters", "full_command", "file_path", "error", "body", "body_ref"} {
		if contentAttribute(activity.Attributes, field) != activity.Content {
			continue
		}
		evidence.Fields = []string{field}
		switch field {
		case "prompt", "response":
			evidence.Kind = field
		case "tool_input", "tool_parameters", "full_command":
			evidence.Kind = "tool_input"
		case "file_path", "body_ref":
			evidence.Kind, evidence.Evidence, evidence.Availability = "reference", "reference", "not_reported"
		case "body":
			if name == "api_request_body" {
				evidence.Kind, evidence.Evidence = "model_input", "explicit_model_input"
			} else if name == "api_response_body" {
				evidence.Kind = "response"
			}
			truncated := activity.Attributes["body_truncated"]
			evidence.Truncated = truncated == true || truncated == "true"
		}
		return evidence
	}
	return evidence
}

func describeCodexContent(activity Activity, evidence ContentEvidence) ContentEvidence {
	name := contentEventName(activity)
	prompt := contentAttribute(activity.Attributes, "prompt")
	if name == "user_prompt" && activity.Content == "" {
		evidence.Kind = "prompt"
		return evidence
	}
	if prompt != "" && prompt == activity.Content {
		evidence.Kind, evidence.Fields = "prompt", []string{"prompt"}
		if prompt == "[REDACTED]" {
			evidence.Availability, evidence.RedactionReason = "redacted", "producer_redacted"
		}
		return evidence
	}
	if name == "user_prompt" && activity.Content == "[REDACTED]" {
		evidence.Kind, evidence.Availability, evidence.RedactionReason = "prompt", "redacted", "producer_redacted"
		return evidence
	}
	arguments := contentArguments(activity.Attributes["arguments"])
	message := contentAttribute(arguments, "message")
	output := contentAttribute(activity.Attributes, "output")
	if message == "" && output == "" {
		return evidence
	}
	encrypted := strings.HasPrefix(message, "gAAAA")
	if encrypted && strings.Contains(activity.Content, message) {
		evidence.Kind, evidence.Availability, evidence.RedactionReason = "tool_input", "redacted", "encrypted_input"
		evidence.Fields = []string{"arguments.message"}
		return evidence
	}
	parts := make([]string, 0, 2)
	if message != "" {
		if encrypted {
			parts = append(parts, "Instruction content encrypted by source telemetry")
		} else {
			parts = append(parts, message)
		}
	}
	if output != "" {
		parts = append(parts, "Result: "+output)
	}
	if activity.Content != strings.Join(parts, "\n") {
		if message == "" && activity.Content == output {
			evidence.Kind, evidence.Evidence, evidence.Fields = "tool_output", "read_output", []string{"output"}
		}
		return evidence
	}
	if message != "" {
		evidence.Kind, evidence.Fields = "tool_input", []string{"arguments.message"}
		if encrypted {
			evidence.Availability, evidence.RedactionReason = "redacted", "encrypted_input"
		}
	}
	if output != "" {
		evidence.Fields = append(evidence.Fields, "output")
		evidence.Kind, evidence.Evidence, evidence.Availability = "tool_output", "read_output", "available"
		if message != "" && !encrypted {
			evidence.Kind, evidence.Evidence = "tool_input_output", "unknown"
		}
	}
	return evidence
}

func contentEventName(activity Activity) string {
	name := contentAttribute(activity.Attributes, "event.name")
	if name == "" {
		name = activity.Name
	}
	for _, prefix := range []string{"claude_code.", "codex.", "gen_ai."} {
		name = strings.TrimPrefix(name, prefix)
	}
	return name
}

func contentAttribute(attributes map[string]any, name string) string {
	value, _ := attributes[name].(string)
	return value
}

func contentArguments(value any) map[string]any {
	if object, ok := value.(map[string]any); ok {
		return object
	}
	var object map[string]any
	if text, ok := value.(string); ok {
		_ = json.Unmarshal([]byte(text), &object)
	}
	return object
}

// ContentForDelivery keeps explicit redaction evidence out of the readable body.
// Transport opt-in is a separate concern owned by each adapter.
func ContentForDelivery(activity Activity) (string, ContentEvidence) {
	evidence := DescribeActivityContent(activity)
	if evidence.Availability == "redacted" {
		return "", evidence
	}
	return activity.Content, evidence
}
