package query

import (
	"errors"
	"testing"
	"time"

	"github.com/kotokumu/agentmetry/internal/canonical"
)

func TestParseTraceIDNormalizesValidOTLPIdentity(t *testing.T) {
	got, err := ParseTraceID("ABCDEFABCDEFABCDEFABCDEFABCDEFAB")
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "abcdefabcdefabcdefabcdefabcdefab" {
		t.Fatalf("trace ID = %q", got.String())
	}
}

func TestParseTraceIDRejectsMalformedAndZeroIdentities(t *testing.T) {
	for _, value := range []string{"", "not-a-trace-id", "00000000000000000000000000000000"} {
		if _, err := ParseTraceID(value); !errors.Is(err, ErrInvalidTraceID) {
			t.Fatalf("ParseTraceID(%q) error = %v, want ErrInvalidTraceID", value, err)
		}
	}
}

func TestParseSpanIDNormalizesAndValidatesOTLPIdentity(t *testing.T) {
	got, err := ParseSpanID("ABCDEFABCDEFABCD")
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "abcdefabcdefabcd" {
		t.Fatalf("span ID = %q", got.String())
	}
	for _, value := range []string{"", "not-a-span-id", "0000000000000000"} {
		if _, err := ParseSpanID(value); !errors.Is(err, ErrInvalidSpanID) {
			t.Fatalf("ParseSpanID(%q) error = %v, want ErrInvalidSpanID", value, err)
		}
	}
}

func TestValidateTraceWindow(t *testing.T) {
	type args struct {
		window TraceWindow
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{name: "no filters", args: args{}},
		{name: "closed instant", args: args{window: TraceWindow{StartedAt: new(time.Unix(1, 0)), EndedAt: new(time.Unix(1, 0)), Kind: canonical.ActivityTool, ErrorsOnly: true}}},
		{name: "start only", args: args{window: TraceWindow{StartedAt: new(time.Unix(1, 0))}}, wantErr: true},
		{name: "end only", args: args{window: TraceWindow{EndedAt: new(time.Unix(1, 0))}}, wantErr: true},
		{name: "zero timestamp", args: args{window: TraceWindow{StartedAt: new(time.Time{}), EndedAt: new(time.Unix(1, 0))}}, wantErr: true},
		{name: "reversed", args: args{window: TraceWindow{StartedAt: new(time.Unix(2, 0)), EndedAt: new(time.Unix(1, 0))}}, wantErr: true},
		{name: "unsupported kind", args: args{window: TraceWindow{Kind: canonical.ActivityKind("artifact")}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateTraceWindow(tt.args.window); (err != nil) != tt.wantErr {
				t.Errorf("ValidateTraceWindow() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTraceWindowIncludes(t *testing.T) {
	type args struct {
		window   TraceWindow
		activity TraceOverviewActivity
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{name: "long span intersects range start", args: args{window: TraceWindow{StartedAt: new(time.Unix(5, 0)), EndedAt: new(time.Unix(6, 0))}, activity: TraceOverviewActivity{StartedAt: time.Unix(1, 0), EndedAt: time.Unix(5, 0)}}, want: true},
		{name: "long span contains range", args: args{window: TraceWindow{StartedAt: new(time.Unix(5, 0)), EndedAt: new(time.Unix(6, 0))}, activity: TraceOverviewActivity{StartedAt: time.Unix(1, 0), EndedAt: time.Unix(9, 0)}}, want: true},
		{name: "ends before range", args: args{window: TraceWindow{StartedAt: new(time.Unix(5, 0)), EndedAt: new(time.Unix(6, 0))}, activity: TraceOverviewActivity{StartedAt: time.Unix(1, 0), EndedAt: time.Unix(4, 999999999)}}},
		{name: "starts after range", args: args{window: TraceWindow{StartedAt: new(time.Unix(5, 0)), EndedAt: new(time.Unix(6, 0))}, activity: TraceOverviewActivity{StartedAt: time.Unix(6, 1), EndedAt: time.Unix(7, 0)}}},
		{name: "kind matches", args: args{window: TraceWindow{Kind: canonical.ActivityTool}, activity: TraceOverviewActivity{Kind: canonical.ActivityTool}}, want: true},
		{name: "kind differs", args: args{window: TraceWindow{Kind: canonical.ActivityTool}, activity: TraceOverviewActivity{Kind: canonical.ActivityResponse}}},
		{name: "observed error", args: args{window: TraceWindow{ErrorsOnly: true}, activity: TraceOverviewActivity{Status: "error"}}, want: true},
		{name: "unknown outcome excluded", args: args{window: TraceWindow{ErrorsOnly: true}, activity: TraceOverviewActivity{Status: "unknown"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TraceWindowIncludes(tt.args.window, tt.args.activity); got != tt.want {
				t.Errorf("TraceWindowIncludes() = %v, want %v", got, tt.want)
			}
		})
	}
}
