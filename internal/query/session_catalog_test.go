package query

import (
	"github.com/google/go-cmp/cmp"
	"testing"
)

func TestParseSessionListView(t *testing.T) {
	type args struct {
		value string
	}
	tests := []struct {
		name    string
		args    args
		want    SessionListView
		wantErr bool
	}{
		{name: "default roots", args: args{value: ""}, want: SessionListRoots},
		{name: "explicit roots", args: args{value: "roots"}, want: SessionListRoots},
		{name: "all", args: args{value: "all"}, want: SessionListAll},
		{name: "unknown", args: args{value: "other"}, wantErr: true},
		{name: "no implicit coercion", args: args{value: " ALL "}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSessionListView(tt.args.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseSessionListView() error = %v, wantErr %v", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ParseSessionListView() mismatch (-want +got): %s", diff)
			}
		})
	}
}

func TestSessionListEntry_Role(t *testing.T) {
	type fields struct {
		Session         Session
		RootSessionID   string
		ParentSessionID string
	}
	tests := []struct {
		name   string
		fields fields
		want   SessionRole
	}{
		{name: "no resolved parent", fields: fields{Session: Session{ID: "r", SourceID: "codex"}, RootSessionID: "r"}, want: SessionRoot},
		{name: "resolved parent", fields: fields{Session: Session{ID: "g", SourceID: "codex"}, RootSessionID: "r", ParentSessionID: "c"}, want: SessionChild},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := SessionListEntry{
				Session:         tt.fields.Session,
				RootSessionID:   tt.fields.RootSessionID,
				ParentSessionID: tt.fields.ParentSessionID,
			}
			if diff := cmp.Diff(tt.want, entry.Role()); diff != "" {
				t.Errorf("SessionListEntry.Role() mismatch (-want +got): %s", diff)
			}
		})
	}
}
