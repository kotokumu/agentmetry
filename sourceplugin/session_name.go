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

// SessionNamesExtractor optionally interprets multiple target conversations in
// one source-owned event. Targets need not be the event's executor.
type SessionNamesExtractor interface {
	SessionNames(Event) []SessionName
}

// SessionNames prefers the plural contract and otherwise preserves old plugins.
func (registry Registry) SessionNames(sourceID string, event Event) []SessionName {
	for _, plugin := range registry.plugins {
		if plugin.ID() != sourceID {
			continue
		}
		if extractor, ok := plugin.(SessionNamesExtractor); ok {
			return extractor.SessionNames(CloneEvent(event))
		}
		if extractor, ok := plugin.(SessionNameExtractor); ok {
			if name, valid := extractor.SessionName(CloneEvent(event)); valid {
				return []SessionName{name}
			}
		}
		break
	}
	return nil
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
