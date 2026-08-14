package query

import (
	"context"
	"errors"
)

var ErrConversationNotFound = errors.New("conversation not found")
var ErrConversationTargetNotFound = errors.New("conversation target not found")

type ConversationFilter struct {
	Identity ConversationIdentity
	Anchor   ActivityAnchor
	Page     Page
	PageMode ConversationPageMode
}

type ConversationPageMode uint8

const (
	ConversationPageAroundAnchor ConversationPageMode = iota
	ConversationPageFromOffset
)

type ConversationReader interface {
	GetConversation(context.Context, ConversationFilter) (Session, error)
}
