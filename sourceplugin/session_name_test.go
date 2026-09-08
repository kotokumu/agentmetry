package sourceplugin

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

type nameTestPlugin struct {
	Plugin
	id string
}

func (plugin nameTestPlugin) ID() string { return plugin.id }

func (plugin nameTestPlugin) SessionName(event Event) (SessionName, bool) {
	event.Attributes["changed"] = true
	return SessionName{ConversationID: "conversation", Text: event.Name, Origin: plugin.id}, true
}

type namelessTestPlugin struct{ Plugin }

func (namelessTestPlugin) ID() string { return "nameless" }

func TestRegistry_SessionName(t *testing.T) {
	type fields struct {
		plugins []Plugin
	}
	type args struct {
		sourceID string
		event    Event
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   SessionName
		want1  bool
	}{
		{
			name:   "no plugins",
			fields: fields{},
			args:   args{sourceID: "missing"},
			want:   SessionName{},
			want1:  false,
		},
		{
			name:   "unknown source does not use other plugin",
			fields: fields{plugins: []Plugin{nameTestPlugin{id: "other"}}},
			args:   args{sourceID: "missing", event: Event{Name: "Name", Attributes: map[string]any{}}},
			want:   SessionName{},
			want1:  false,
		},
		{
			name:   "optional contract absent",
			fields: fields{plugins: []Plugin{namelessTestPlugin{}}},
			args:   args{sourceID: "nameless"},
			want:   SessionName{},
			want1:  false,
		},
		{
			name:   "only owning plugin extracts",
			fields: fields{plugins: []Plugin{nameTestPlugin{id: "other"}, nameTestPlugin{id: "owner"}}},
			args:   args{sourceID: "owner", event: Event{Name: "Name", Attributes: map[string]any{}}},
			want:   SessionName{ConversationID: "conversation", Text: "Name", Origin: "owner"},
			want1:  true,
		},
		{
			name:   "first owner without optional contract ends lookup",
			fields: fields{plugins: []Plugin{namelessTestPlugin{}, nameTestPlugin{id: "nameless"}}},
			args:   args{sourceID: "nameless", event: Event{Name: "Name", Attributes: map[string]any{}}},
			want:   SessionName{},
			want1:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := Registry{
				plugins: tt.fields.plugins,
			}
			got, got1 := registry.SessionName(tt.args.sourceID, tt.args.event)
			if !cmp.Equal(tt.want, got) {
				t.Errorf("Registry.SessionName() got = %v, want %v\ndiff=%s", got, tt.want, cmp.Diff(tt.want, got))
			}
			if got1 != tt.want1 {
				t.Errorf("Registry.SessionName() got1 = %v, want %v", got1, tt.want1)
			}
			if diff := cmp.Diff(false, tt.args.event.Attributes["changed"] == true); diff != "" {
				t.Errorf("input attributes mutated (-want +got): %s", diff)
			}
		})
	}
}

type namesTestPlugin struct{ nameTestPlugin }

func (plugin namesTestPlugin) SessionNames(event Event) []SessionName {
	event.Attributes["changed"] = true
	return []SessionName{{ConversationID: "a", Text: event.Name, Origin: plugin.id}, {ConversationID: "b", Text: "Second", Origin: plugin.id}}
}

func TestRegistry_SessionNames(t *testing.T) {
	type fields struct {
		plugins []Plugin
	}
	type args struct {
		sourceID string
		event    Event
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   []SessionName
	}{
		{name: "no plugins", args: args{sourceID: "missing"}, want: nil},
		{name: "unknown owner", fields: fields{plugins: []Plugin{nameTestPlugin{id: "other"}}}, args: args{sourceID: "missing"}, want: nil},
		{name: "optional absent", fields: fields{plugins: []Plugin{namelessTestPlugin{}}}, args: args{sourceID: "nameless"}, want: nil},
		{name: "singular fallback", fields: fields{plugins: []Plugin{nameTestPlugin{id: "owner"}}}, args: args{sourceID: "owner", event: Event{Name: "Name", Attributes: map[string]any{}}}, want: []SessionName{{ConversationID: "conversation", Text: "Name", Origin: "owner"}}},
		{name: "plural preferred without singular duplication", fields: fields{plugins: []Plugin{nameTestPlugin{id: "other"}, namesTestPlugin{nameTestPlugin{id: "owner"}}}}, args: args{sourceID: "owner", event: Event{Name: "Name", Attributes: map[string]any{}}}, want: []SessionName{{ConversationID: "a", Text: "Name", Origin: "owner"}, {ConversationID: "b", Text: "Second", Origin: "owner"}}},
		{name: "first owner ends lookup", fields: fields{plugins: []Plugin{namelessTestPlugin{}, nameTestPlugin{id: "nameless"}}}, args: args{sourceID: "nameless"}, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := Registry{
				plugins: tt.fields.plugins,
			}
			if got := registry.SessionNames(tt.args.sourceID, tt.args.event); !cmp.Equal(tt.want, got) {
				t.Errorf("Registry.SessionNames() = %v, want %v\ndiff=%s", got, tt.want, cmp.Diff(tt.want, got))
			}
			if diff := cmp.Diff(false, tt.args.event.Attributes["changed"] == true); diff != "" {
				t.Errorf("input mutated: %s", diff)
			}
		})
	}
}
