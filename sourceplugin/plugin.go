// Package sourceplugin defines the producer-neutral extension contract used to
// interpret AI agent telemetry without coupling ingestion to a specific source.
package sourceplugin

const Unknown = "unknown"

// Event is the source-plugin boundary. Attributes are copied before a plugin
// receives them, so normalization never mutates decoded OTLP data.
type Event struct {
	Name       string
	Attributes map[string]any
}

// ProfiledEvent is an event enriched by one selected source plugin.
type ProfiledEvent struct {
	Source string
	Event
}

// Plugin detects and semantically normalizes one telemetry producer.
type Plugin interface {
	ID() string
	Match(Event) bool
	Normalize(Event) Event
}

// AgentMetadata is the producer-neutral description assembled from stored
// telemetry. Runtime identity, reusable definition, execution type, and model
// remain separate because producers may report them on different records.
type AgentMetadata struct {
	ID, Definition, Type, ParentID, Model string
}

// AgentMetadataNormalizer is an optional read-time compatibility contract for
// data stored before a plugin learned a newer semantic mapping.
type AgentMetadataNormalizer interface {
	NormalizeAgentMetadata(AgentMetadata) AgentMetadata
}

type SourceDescriptor struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type SourceDescriber interface {
	DisplayName() string
}

// Registry applies plugins in registration order. The first match wins.
type Registry struct {
	plugins []Plugin
}

func NewRegistry(plugins ...Plugin) Registry {
	return Registry{plugins: append([]Plugin(nil), plugins...)}
}

func (registry Registry) Profile(event Event) ProfiledEvent {
	for _, plugin := range registry.plugins {
		if plugin.Match(event) {
			return ProfiledEvent{Source: plugin.ID(), Event: plugin.Normalize(CloneEvent(event))}
		}
	}
	return ProfiledEvent{Source: Unknown, Event: CloneEvent(event)}
}

// NormalizeAgentMetadata applies only the plugin that owns the stored source.
func (registry Registry) NormalizeAgentMetadata(sourceID string, metadata AgentMetadata) AgentMetadata {
	for _, plugin := range registry.plugins {
		if plugin.ID() != sourceID {
			continue
		}
		if normalizer, ok := plugin.(AgentMetadataNormalizer); ok {
			return normalizer.NormalizeAgentMetadata(metadata)
		}
		break
	}
	return metadata
}

func (registry Registry) Describe(sourceID string) SourceDescriptor {
	descriptor := SourceDescriptor{ID: sourceID, Label: sourceID}
	for _, plugin := range registry.plugins {
		if plugin.ID() != sourceID {
			continue
		}
		if describer, ok := plugin.(SourceDescriber); ok && describer.DisplayName() != "" {
			descriptor.Label = describer.DisplayName()
		}
		break
	}
	return descriptor
}

func CloneEvent(event Event) Event {
	attributes := make(map[string]any, len(event.Attributes))
	for key, value := range event.Attributes {
		attributes[key] = value
	}
	return Event{Name: event.Name, Attributes: attributes}
}
