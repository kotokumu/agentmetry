package sourceplugin

import "time"

// SessionName is a name observed in producer telemetry, not conversation identity.
type SessionName struct {
	ConversationID string
	Text           string
	Origin         string
	ObservedAt     time.Time
}

// SessionNameExtractor optionally interprets name evidence owned by a source.
type SessionNameExtractor interface {
	SessionName(Event) (SessionName, bool)
}

// SessionName delegates only to the plugin that owns the stored source.
func (registry Registry) SessionName(sourceID string, event Event) (SessionName, bool) {
	for _, plugin := range registry.plugins {
		if plugin.ID() != sourceID {
			continue
		}
		if extractor, ok := plugin.(SessionNameExtractor); ok {
			return extractor.SessionName(CloneEvent(event))
		}
		break
	}
	return SessionName{}, false
}
