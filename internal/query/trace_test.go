package query

import (
	"errors"
	"testing"
)

func TestParseTraceIDNormalizesValidOTLPIdentity(t *testing.T) {
	got, err := ParseTraceID("ABCDEFABCDEFABCDEFABCDEFABCDEFAB")
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "abcdefabcdefabcdefabcdefabcdefab" {
		t.Fatalf("trace ID = %q", got.String())
	}
}

func TestParseTraceIDRejectsMalformedAndZeroIdentities(t *testing.T) {
	for _, value := range []string{"", "not-a-trace-id", "00000000000000000000000000000000"} {
		if _, err := ParseTraceID(value); !errors.Is(err, ErrInvalidTraceID) {
			t.Fatalf("ParseTraceID(%q) error = %v, want ErrInvalidTraceID", value, err)
		}
	}
}

func TestParseSpanIDNormalizesAndValidatesOTLPIdentity(t *testing.T) {
	got, err := ParseSpanID("ABCDEFABCDEFABCD")
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "abcdefabcdefabcd" {
		t.Fatalf("span ID = %q", got.String())
	}
	for _, value := range []string{"", "not-a-span-id", "0000000000000000"} {
		if _, err := ParseSpanID(value); !errors.Is(err, ErrInvalidSpanID) {
			t.Fatalf("ParseSpanID(%q) error = %v, want ErrInvalidSpanID", value, err)
		}
	}
}
