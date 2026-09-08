package claude

import "strconv"

type CallAliasKind string

const (
	CallAliasClientRequestID CallAliasKind = "client_request_id"
	CallAliasRequestID       CallAliasKind = "request_id"
	CallAliasEventSequence   CallAliasKind = "event_sequence"
)

type CallAlias struct {
	Kind     CallAliasKind
	Value    string
	Sequence uint64
}

// CallIdentityAliases extracts every valid provider identity from normalized
// Claude attributes. Consumers retain the full set so a later bridge event can
// merge previously separate identity components.
func CallIdentityAliases(attributes map[string]any) []CallAlias {
	var result []CallAlias
	if value := firstNonEmptyString(attributes, "gen_ai.client.request.id", "client_request_id"); value != "" {
		result = append(result, CallAlias{Kind: CallAliasClientRequestID, Value: value})
	}
	if value := firstNonEmptyString(attributes, "gen_ai.request.id", "request_id"); value != "" {
		result = append(result, CallAlias{Kind: CallAliasRequestID, Value: value})
	}
	if value, ok := attributes["event.sequence"].(int64); ok && value >= 0 {
		result = append(result, CallAlias{Kind: CallAliasEventSequence, Sequence: uint64(value)})
	}
	return result
}

func firstNonEmptyString(attributes map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := attributes[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func (alias CallAlias) StorageValue() string {
	if alias.Kind == CallAliasEventSequence {
		return strconv.FormatUint(alias.Sequence, 10)
	}
	return alias.Value
}
