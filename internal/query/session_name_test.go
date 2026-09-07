package query

import (
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestSelectSessionName(t *testing.T) {
	type args struct {
		names []SessionName
	}
	tests := []struct {
		name string
		args args
		want *SessionName
	}{
		{
			name: "no evidence",
			args: args{},
			want: nil,
		},
		{
			name: "blank evidence is ignored",
			args: args{names: []SessionName{{Text: " \t", Origin: "generated"}, {Text: "Name", Origin: " "}}},
			want: nil,
		},
		{
			name: "single unknown timestamp",
			args: args{names: []SessionName{{Text: "Name", Origin: "generated"}}},
			want: &SessionName{Text: "Name", Origin: "generated"},
		},
		{
			name: "preserve original text",
			args: args{names: []SessionName{{Text: " 名前 <b> </b> ", Origin: "generated"}}},
			want: &SessionName{Text: " 名前 <b> </b> ", Origin: "generated"},
		},
		{
			name: "latest known supersedes old conflict",
			args: args{names: []SessionName{{Text: "Old", Origin: "generated", ObservedAt: time.Unix(1, 0)}, {Text: "New", Origin: "generated", ObservedAt: time.Unix(2, 0)}}},
			want: &SessionName{Text: "New", Origin: "generated", ObservedAt: time.Unix(2, 0)},
		},
		{
			name: "reverse arrival order",
			args: args{names: []SessionName{{Text: "New", Origin: "generated", ObservedAt: time.Unix(2, 0)}, {Text: "Old", Origin: "generated", ObservedAt: time.Unix(1, 0)}}},
			want: &SessionName{Text: "New", Origin: "generated", ObservedAt: time.Unix(2, 0)},
		},
		{
			name: "same time conflicting names",
			args: args{names: []SessionName{{Text: "First", Origin: "generated", ObservedAt: time.Unix(2, 0)}, {Text: "Second", Origin: "generated", ObservedAt: time.Unix(2, 0)}}},
			want: nil,
		},
		{
			name: "same name different origin",
			args: args{names: []SessionName{{Text: "Name", Origin: "generated", ObservedAt: time.Unix(2, 0)}, {Text: "Name", Origin: "snapshot", ObservedAt: time.Unix(2, 0)}}},
			want: nil,
		},
		{
			name: "unknown agrees with latest",
			args: args{names: []SessionName{{Text: "Old", Origin: "generated", ObservedAt: time.Unix(1, 0)}, {Text: "New", Origin: "generated", ObservedAt: time.Unix(2, 0)}, {Text: "New", Origin: "generated"}}},
			want: &SessionName{Text: "New", Origin: "generated"},
		},
		{
			name: "unknown before latest",
			args: args{names: []SessionName{{Text: "New", Origin: "generated"}, {Text: "Old", Origin: "generated", ObservedAt: time.Unix(1, 0)}, {Text: "New", Origin: "generated", ObservedAt: time.Unix(2, 0)}}},
			want: &SessionName{Text: "New", Origin: "generated"},
		},
		{
			name: "unknown between old and latest",
			args: args{names: []SessionName{{Text: "Old", Origin: "generated", ObservedAt: time.Unix(1, 0)}, {Text: "New", Origin: "generated"}, {Text: "New", Origin: "generated", ObservedAt: time.Unix(2, 0)}}},
			want: &SessionName{Text: "New", Origin: "generated"},
		},
		{
			name: "latest before old and unknown",
			args: args{names: []SessionName{{Text: "New", Origin: "generated", ObservedAt: time.Unix(2, 0)}, {Text: "Old", Origin: "generated", ObservedAt: time.Unix(1, 0)}, {Text: "New", Origin: "generated"}}},
			want: &SessionName{Text: "New", Origin: "generated"},
		},
		{
			name: "latest then unknown then old",
			args: args{names: []SessionName{{Text: "New", Origin: "generated", ObservedAt: time.Unix(2, 0)}, {Text: "New", Origin: "generated"}, {Text: "Old", Origin: "generated", ObservedAt: time.Unix(1, 0)}}},
			want: &SessionName{Text: "New", Origin: "generated"},
		},
		{
			name: "unknown then latest then old",
			args: args{names: []SessionName{{Text: "New", Origin: "generated"}, {Text: "New", Origin: "generated", ObservedAt: time.Unix(2, 0)}, {Text: "Old", Origin: "generated", ObservedAt: time.Unix(1, 0)}}},
			want: &SessionName{Text: "New", Origin: "generated"},
		},
		{
			name: "unknown conflicts with latest",
			args: args{names: []SessionName{{Text: "Old", Origin: "generated"}, {Text: "New", Origin: "generated", ObservedAt: time.Unix(2, 0)}}},
			want: nil,
		},
		{
			name: "unknown conflicts even when old known agrees",
			args: args{names: []SessionName{{Text: "Old", Origin: "generated", ObservedAt: time.Unix(1, 0)}, {Text: "Old", Origin: "generated"}, {Text: "New", Origin: "generated", ObservedAt: time.Unix(2, 0)}}},
			want: nil,
		},
		{
			name: "duplicate observations",
			args: args{names: []SessionName{{Text: "Name", Origin: "generated", ObservedAt: time.Unix(2, 0)}, {Text: "Name", Origin: "generated", ObservedAt: time.Unix(2, 0)}}},
			want: &SessionName{Text: "Name", Origin: "generated", ObservedAt: time.Unix(2, 0)},
		},
		{
			name: "all unknown agree",
			args: args{names: []SessionName{{Text: "Name", Origin: "generated"}, {Text: "Name", Origin: "generated"}}},
			want: &SessionName{Text: "Name", Origin: "generated"},
		},
		{
			name: "all unknown conflict",
			args: args{names: []SessionName{{Text: "One", Origin: "generated"}, {Text: "Two", Origin: "generated"}}},
			want: nil,
		},
		{
			name: "old conflicting ties do not matter",
			args: args{names: []SessionName{{Text: "First", Origin: "generated", ObservedAt: time.Unix(1, 0)}, {Text: "Second", Origin: "generated", ObservedAt: time.Unix(1, 0)}, {Text: "New", Origin: "generated", ObservedAt: time.Unix(2, 0)}}},
			want: &SessionName{Text: "New", Origin: "generated", ObservedAt: time.Unix(2, 0)},
		},
		{
			name: "invalid newer candidate cannot displace valid name",
			args: args{names: []SessionName{{Text: "Name", Origin: "generated", ObservedAt: time.Unix(1, 0)}, {Text: "", Origin: "generated", ObservedAt: time.Unix(2, 0)}}},
			want: &SessionName{Text: "Name", Origin: "generated", ObservedAt: time.Unix(1, 0)},
		},
		{
			name: "different unknown origin conflicts",
			args: args{names: []SessionName{{Text: "Name", Origin: "generated", ObservedAt: time.Unix(2, 0)}, {Text: "Name", Origin: "snapshot"}}},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := slices.Clone(tt.args.names)
			if got := SelectSessionName(tt.args.names); !cmp.Equal(tt.want, got) {
				t.Errorf("SelectSessionName() = %v, want %v\ndiff=%s", got, tt.want, cmp.Diff(tt.want, got))
			}
			if diff := cmp.Diff(original, tt.args.names); diff != "" {
				t.Errorf("input changed (-want +got): %s", diff)
			}
		})
	}
}
