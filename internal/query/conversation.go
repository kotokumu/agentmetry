package query

import (
	"context"
	"errors"
)

var ErrConversationNotFound = errors.New("conversation not found")
var ErrConversationTargetNotFound = errors.New("conversation target not found")

type ConversationFilter struct {
	SourceID          string
	ConversationID    string
	TraceID           string
	SpanID            string
	ActivityOffset    int
	ActivityLimit     int
	UseActivityOffset bool
}

type ConversationReader interface {
	GetConversation(context.Context, ConversationFilter) (Session, error)
}
