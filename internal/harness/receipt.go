package harness

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	scopePattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	fingerprintPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// MetadataValues contains the raw values for the three allowlisted OTLP
// metadata fields. Transports preserve repeated values so validation is
// identical for HTTP and gRPC.
type MetadataValues struct {
	Scope       []string
	Fingerprint []string
	Label       []string
}

type ReceiptState string

const (
	ReceiptUnreported ReceiptState = "unreported"
	ReceiptReported   ReceiptState = "reported"
	ReceiptInvalid    ReceiptState = "invalid"
)

type ReceiptEvidence struct {
	State       ReceiptState
	Scope       string
	Fingerprint string
	Label       string
}

func (evidence ReceiptEvidence) Valid() bool {
	switch evidence.State {
	case ReceiptUnreported, ReceiptInvalid:
		return evidence.Scope == "" && evidence.Fingerprint == "" && evidence.Label == ""
	case ReceiptReported:
		return ValidScope(evidence.Scope) && ValidFingerprint(evidence.Fingerprint) &&
			evidence.Label == strings.TrimSpace(evidence.Label) && ValidLabel(evidence.Label)
	default:
		return false
	}
}

func ParseMetadata(values MetadataValues) ReceiptEvidence {
	if len(values.Scope) > 1 || len(values.Fingerprint) > 1 || len(values.Label) > 1 {
		return ReceiptEvidence{State: ReceiptInvalid}
	}
	scope := singleValue(values.Scope)
	fingerprint := singleValue(values.Fingerprint)
	label := singleValue(values.Label)
	if scope == "" && fingerprint == "" && label == "" {
		return ReceiptEvidence{State: ReceiptUnreported}
	}
	if strings.Contains(scope, ",") || strings.Contains(fingerprint, ",") || strings.Contains(label, ",") {
		return ReceiptEvidence{State: ReceiptInvalid}
	}
	label = strings.TrimSpace(label)
	if !ValidScope(scope) || !ValidFingerprint(fingerprint) || !ValidLabel(label) {
		return ReceiptEvidence{State: ReceiptInvalid}
	}
	return ReceiptEvidence{State: ReceiptReported, Scope: scope, Fingerprint: fingerprint, Label: label}
}

func ValidScope(value string) bool {
	return scopePattern.MatchString(value)
}

func ValidFingerprint(value string) bool {
	return fingerprintPattern.MatchString(value)
}

func ValidLabel(value string) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > 80 || strings.Contains(value, ",") {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func singleValue(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
