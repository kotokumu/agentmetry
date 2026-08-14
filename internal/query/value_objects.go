package query

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const (
	maxSourceIDLength       = 100
	maxConversationIDLength = 500
	defaultPageSize         = 100
	maxPageSize             = 100
	anchorContextItems      = 25
)

var (
	ErrInvalidConversationIdentity = errors.New("invalid conversation identity")
	ErrInvalidTraceID              = errors.New("invalid OTLP trace ID")
	ErrInvalidSpanID               = errors.New("invalid OTLP span ID")
	ErrInvalidActivityAnchor       = errors.New("invalid activity anchor")
	ErrInvalidPage                 = errors.New("invalid page")
	ErrInvalidTimelineDirection    = errors.New("invalid timeline direction")
)

// ConversationIdentity identifies a conversation within one telemetry source.
// Its private representation prevents source-unqualified identities from
// crossing the query boundary.
type ConversationIdentity struct {
	sourceID       string
	conversationID string
}

func NewConversationIdentity(sourceID, conversationID string) (ConversationIdentity, error) {
	sourceID = strings.TrimSpace(sourceID)
	conversationID = strings.TrimSpace(conversationID)
	if sourceID == "" || conversationID == "" {
		return ConversationIdentity{}, fmt.Errorf("%w: source and conversation identities are required", ErrInvalidConversationIdentity)
	}
	if len(sourceID) > maxSourceIDLength || len(conversationID) > maxConversationIDLength {
		return ConversationIdentity{}, fmt.Errorf("%w: source must be at most %d and conversation at most %d characters", ErrInvalidConversationIdentity, maxSourceIDLength, maxConversationIDLength)
	}
	return ConversationIdentity{sourceID: sourceID, conversationID: conversationID}, nil
}

func (identity ConversationIdentity) SourceID() string       { return identity.sourceID }
func (identity ConversationIdentity) ConversationID() string { return identity.conversationID }

type TraceID struct{ value string }

func ParseTraceID(value string) (TraceID, error) {
	parsed, err := parseOTLPID(value, 32, ErrInvalidTraceID)
	if err != nil {
		return TraceID{}, err
	}
	return TraceID{value: parsed}, nil
}

func (identity TraceID) String() string { return identity.value }

type SpanID struct{ value string }

func ParseSpanID(value string) (SpanID, error) {
	parsed, err := parseOTLPID(value, 16, ErrInvalidSpanID)
	if err != nil {
		return SpanID{}, err
	}
	return SpanID{value: parsed}, nil
}

func (identity SpanID) String() string { return identity.value }

func parseOTLPID(value string, length int, kind error) (string, error) {
	if len(value) != length || strings.TrimSpace(value) != value {
		return "", fmt.Errorf("%w: expected %d hexadecimal characters", kind, length)
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return "", fmt.Errorf("%w: expected %d hexadecimal characters", kind, length)
	}
	for _, part := range decoded {
		if part != 0 {
			return strings.ToLower(value), nil
		}
	}
	return "", fmt.Errorf("%w: zero is not a valid identifier", kind)
}

// ActivityAnchor is an optional, complete trace/span location.
type ActivityAnchor struct {
	traceID TraceID
	spanID  SpanID
}

func NewActivityAnchor(traceID, spanID string) (ActivityAnchor, error) {
	traceID = strings.TrimSpace(traceID)
	spanID = strings.TrimSpace(spanID)
	if traceID == "" && spanID == "" {
		return ActivityAnchor{}, nil
	}
	if traceID == "" || spanID == "" {
		return ActivityAnchor{}, fmt.Errorf("%w: trace and span identities must be provided together", ErrInvalidActivityAnchor)
	}
	parsedTraceID, err := ParseTraceID(traceID)
	if err != nil {
		return ActivityAnchor{}, fmt.Errorf("%w: %w", ErrInvalidActivityAnchor, err)
	}
	parsedSpanID, err := ParseSpanID(spanID)
	if err != nil {
		return ActivityAnchor{}, fmt.Errorf("%w: %w", ErrInvalidActivityAnchor, err)
	}
	return ActivityAnchor{traceID: parsedTraceID, spanID: parsedSpanID}, nil
}

func (anchor ActivityAnchor) Present() bool    { return anchor.traceID.String() != "" }
func (anchor ActivityAnchor) TraceID() TraceID { return anchor.traceID }
func (anchor ActivityAnchor) SpanID() SpanID   { return anchor.spanID }

// Page is an immutable, bounded query window. Its zero value is the default
// first page, which keeps zero-value filters useful and valid.
type Page struct {
	offset int
	size   int
}

func NewPage(offset, size int) (Page, error) {
	if offset < 0 {
		return Page{}, fmt.Errorf("%w: offset must be a non-negative integer", ErrInvalidPage)
	}
	if size < 1 || size > maxPageSize {
		return Page{}, fmt.Errorf("%w: size must be between 1 and %d", ErrInvalidPage, maxPageSize)
	}
	return Page{offset: offset, size: size}, nil
}

func (page Page) Offset() int { return page.offset }

func (page Page) Size() int {
	if page.size == 0 {
		return defaultPageSize
	}
	return page.size
}

func (page Page) NextOffset(returnedCount int) int {
	if returnedCount <= 0 {
		return page.offset
	}
	maxInt := int(^uint(0) >> 1)
	if returnedCount > maxInt-page.offset {
		return maxInt
	}
	return page.offset + returnedCount
}

func (page Page) WindowEnd(offset int) int {
	if offset < 0 {
		offset = 0
	}
	maxInt := int(^uint(0) >> 1)
	if page.Size() > maxInt-offset {
		return maxInt
	}
	return offset + page.Size()
}

// OffsetAround keeps bounded preceding context before an anchored activity.
// Large pages retain the established 25-item context; smaller pages center the
// anchor so it cannot fall outside the requested window.
func (page Page) OffsetAround(anchorIndex int) int {
	contextItems := min(anchorContextItems, page.Size()/2)
	return max(0, anchorIndex-contextItems)
}

func (page Page) HasMore(total int64, returnedCount int) bool {
	return int64(page.NextOffset(returnedCount)) < total
}

func (page Page) PreviousOffset() int {
	return max(0, page.offset-page.Size())
}

type TimelineDirection string

const TimelineOlder TimelineDirection = "older"

func ParseTimelineDirection(value string) (TimelineDirection, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || value == string(TimelineOlder) {
		return TimelineOlder, nil
	}
	return "", fmt.Errorf("%w: %q is not supported; use older", ErrInvalidTimelineDirection, value)
}

func (direction TimelineDirection) String() string { return string(direction) }
