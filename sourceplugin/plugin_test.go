package sourceplugin_test

import (
	"testing"

	"github.com/kotokumu/agentmetry/sourceplugin"
)

type testPlugin struct {
	id      string
	matches bool
	name    string
}

func (plugin testPlugin) DisplayName() string { return "Test Source" }

func (plugin testPlugin) ID() string { return plugin.id }

func (plugin testPlugin) Match(sourceplugin.Event) bool { return plugin.matches }

func (plugin testPlugin) Normalize(event sourceplugin.Event) sourceplugin.Event {
	event.Name = plugin.name
	event.Attributes["normalized"] = true
	return event
}

func TestRegistryUsesFirstMatchingPluginWithoutMutatingInput(t *testing.T) {
	registry := sourceplugin.NewRegistry(
		testPlugin{id: "first", matches: true, name: "first.event"},
		testPlugin{id: "second", matches: true, name: "second.event"},
	)
	attributes := map[string]any{"original": "retained"}

	profiled := registry.Profile(sourceplugin.Event{Name: "raw.event", Attributes: attributes})

	if profiled.Source != "first" || profiled.Name != "first.event" || profiled.Attributes["normalized"] != true {
		t.Fatalf("unexpected profiled event: %#v", profiled)
	}
	if _, mutated := attributes["normalized"]; mutated {
		t.Fatalf("registry mutated input attributes: %#v", attributes)
	}
}

func TestRegistryReturnsIndependentGenericEventForUnknownSource(t *testing.T) {
	registry := sourceplugin.NewRegistry(testPlugin{id: "never", matches: false})
	attributes := map[string]any{"event.name": "generic.event"}

	profiled := registry.Profile(sourceplugin.Event{Name: "generic.event", Attributes: attributes})
	profiled.Attributes["changed"] = true

	if profiled.Source != sourceplugin.Unknown || profiled.Name != "generic.event" {
		t.Fatalf("unexpected generic event: %#v", profiled)
	}
	if _, mutated := attributes["changed"]; mutated {
		t.Fatalf("generic fallback mutated input attributes: %#v", attributes)
	}
}

func TestRegistryDescribesSourcesWithoutLeakingProductNamesToConsumers(t *testing.T) {
	registry := sourceplugin.NewRegistry(testPlugin{id: "test", matches: true})

	descriptor := registry.Describe("test")

	if descriptor.ID != "test" || descriptor.Label != "Test Source" {
		t.Fatalf("unexpected source descriptor: %#v", descriptor)
	}
}
