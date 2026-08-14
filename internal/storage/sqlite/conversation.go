package sqlite

import (
	"context"
	"time"

	"github.com/theoden9014/agentmetry/internal/query"
)

// GetConversation returns one source-qualified conversation independently of
// dashboard time filters and display pagination.
func (store *Store) GetConversation(ctx context.Context, filter query.ConversationFilter) (query.Session, error) {
	sourceID := filter.Identity.SourceID()
	conversationID := filter.Identity.ConversationID()
	activities, err := store.activities(ctx, formatTime(time.Time{}), -1, sourceID, conversationID)
	if err != nil {
		return query.Session{}, err
	}
	if len(activities) == 0 {
		return query.Session{}, query.ErrConversationNotFound
	}
	targetRequested := filter.Anchor.Present()
	targetMatches := func(activity query.Activity) bool {
		return activity.Signal == "trace" && activity.TraceID == filter.Anchor.TraceID().String() && activity.SpanID == filter.Anchor.SpanID().String()
	}
	if targetRequested {
		found := false
		for _, activity := range activities {
			if targetMatches(activity) {
				found = true
				break
			}
		}
		if !found {
			return query.Session{}, query.ErrConversationTargetNotFound
		}
	}
	sessions := buildSessions(activities, query.OverviewFilter{
		SourceID:      sourceID,
		ActivityLimit: len(activities),
	}, store.describeSource, func(activity query.Activity) bool {
		return meaningful(activity) || (targetRequested && targetMatches(activity))
	})
	for _, session := range sessions {
		if session.SourceID == sourceID && session.ID == conversationID {
			limit := filter.Page.Size()
			offset := boundedOffset(len(session.Activities), filter.Page.Offset())
			if targetRequested && filter.PageMode == query.ConversationPageAroundAnchor {
				for index, activity := range session.Activities {
					if targetMatches(activity) {
						offset = boundedOffset(len(session.Activities), filter.Page.OffsetAround(index))
						break
					}
				}
			}
			session.ActivityOffset = offset
			session.Activities = activityPage(session.Activities, offset, limit)
			session.HasEarlier = offset > 0
			session.HasMore = int64(offset+len(session.Activities)) < session.ActivityCount
			return session, nil
		}
	}
	return query.Session{}, query.ErrConversationNotFound
}
