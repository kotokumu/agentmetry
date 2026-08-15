package query

import (
	"errors"
	"strings"
	"testing"
)

func TestConversationIdentityNormalizesSourceQualifiedIdentity(t *testing.T) {
	identity, err := NewConversationIdentity("  codex  ", "  run-1  ")
	if err != nil {
		t.Fatal(err)
	}
	if identity.SourceID() != "codex" || identity.ConversationID() != "run-1" {
		t.Fatalf("identity = %q/%q", identity.SourceID(), identity.ConversationID())
	}
}

func TestConversationIdentityRejectsMissingAndOversizedParts(t *testing.T) {
	tests := []struct {
		name           string
		sourceID       string
		conversationID string
	}{
		{name: "missing source", conversationID: "run-1"},
		{name: "missing conversation", sourceID: "codex"},
		{name: "oversized source", sourceID: strings.Repeat("s", 101), conversationID: "run-1"},
		{name: "oversized conversation", sourceID: "codex", conversationID: strings.Repeat("r", 501)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewConversationIdentity(test.sourceID, test.conversationID)
			if !errors.Is(err, ErrInvalidConversationIdentity) {
				t.Fatalf("error = %v, want ErrInvalidConversationIdentity", err)
			}
		})
	}
}

func TestOTLPIdentityValueObjectsNormalizeAndValidate(t *testing.T) {
	traceID, err := ParseTraceID("ABCDEFABCDEFABCDEFABCDEFABCDEFAB")
	if err != nil {
		t.Fatal(err)
	}
	spanID, err := ParseSpanID("ABCDEFABCDEFABCD")
	if err != nil {
		t.Fatal(err)
	}
	if traceID.String() != "abcdefabcdefabcdefabcdefabcdefab" {
		t.Fatalf("trace ID = %q", traceID.String())
	}
	if spanID.String() != "abcdefabcdefabcd" {
		t.Fatalf("span ID = %q", spanID.String())
	}
	if (TraceID{}).String() != "" || (SpanID{}).String() != "" {
		t.Fatal("zero OTLP identities must stringify to empty values")
	}
}

func TestActivityAnchorRequiresBothOTLPIdentities(t *testing.T) {
	anchor, err := NewActivityAnchor("", "")
	if err != nil || anchor.Present() {
		t.Fatalf("empty anchor = %#v, %v", anchor, err)
	}

	for _, values := range [][2]string{
		{"11111111111111111111111111111111", ""},
		{"", "1111111111111111"},
	} {
		_, err := NewActivityAnchor(values[0], values[1])
		if !errors.Is(err, ErrInvalidActivityAnchor) {
			t.Fatalf("NewActivityAnchor(%q, %q) error = %v", values[0], values[1], err)
		}
	}

	anchor, err = NewActivityAnchor("ABCDEFABCDEFABCDEFABCDEFABCDEFAB", "ABCDEFABCDEFABCD")
	if err != nil {
		t.Fatal(err)
	}
	if !anchor.Present() || anchor.TraceID().String() != "abcdefabcdefabcdefabcdefabcdefab" || anchor.SpanID().String() != "abcdefabcdefabcd" {
		t.Fatalf("anchor = %#v", anchor)
	}
}

func TestPageHasSafeDefaultAndOwnsWindowCalculations(t *testing.T) {
	var defaultPage Page
	if defaultPage.Offset() != 0 || defaultPage.Size() != 100 {
		t.Fatalf("default page = offset %d, size %d", defaultPage.Offset(), defaultPage.Size())
	}

	page, err := NewPage(25, 10)
	if err != nil {
		t.Fatal(err)
	}
	if page.Offset() != 25 || page.Size() != 10 || page.NextOffset(7) != 32 || page.WindowEnd(35) != 45 {
		t.Fatalf("page = offset %d, size %d, next %d, window end %d", page.Offset(), page.Size(), page.NextOffset(7), page.WindowEnd(35))
	}
	if !page.HasMore(40, 7) || page.HasMore(32, 7) {
		t.Fatal("page continuation calculation is incorrect")
	}
	if page.PreviousOffset() != 15 {
		t.Fatalf("previous offset = %d", page.PreviousOffset())
	}
	if page.OffsetAround(40) != 35 {
		t.Fatalf("anchor offset = %d", page.OffsetAround(40))
	}
	if defaultPage.OffsetAround(104) != 79 {
		t.Fatalf("default anchor offset = %d", defaultPage.OffsetAround(104))
	}
}

func TestPageRejectsInvalidBounds(t *testing.T) {
	for _, values := range [][2]int{{-1, 10}, {0, 0}, {0, -1}, {0, 101}} {
		_, err := NewPage(values[0], values[1])
		if !errors.Is(err, ErrInvalidPage) {
			t.Fatalf("NewPage(%d, %d) error = %v", values[0], values[1], err)
		}
	}
}

func TestTimelineDirectionUsesQueryVocabulary(t *testing.T) {
	for _, value := range []string{"", "older", "OLDER"} {
		direction, err := ParseTimelineDirection(value)
		if err != nil {
			t.Fatalf("ParseTimelineDirection(%q): %v", value, err)
		}
		if direction != TimelineOlder || direction.String() != "older" {
			t.Fatalf("direction = %q", direction)
		}
	}
	if direction, err := ParseTimelineDirection("newer"); err != nil || direction != TimelineNewer {
		t.Fatalf("newer direction = %q, %v", direction, err)
	}
	if _, err := ParseTimelineDirection("sideways"); !errors.Is(err, ErrInvalidTimelineDirection) {
		t.Fatalf("error = %v, want ErrInvalidTimelineDirection", err)
	}
}
