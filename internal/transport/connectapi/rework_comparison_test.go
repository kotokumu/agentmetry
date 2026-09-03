package connectapi

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "github.com/kotokumu/agentmetry/gen/agentmetry/v1"
	"github.com/kotokumu/agentmetry/internal/query"
)

func Test_mapReworkComparisonValue(t *testing.T) {
	type args struct {
		value query.ReworkComparisonValue
	}
	tests := []struct {
		name string
		args args
		want *v1.ReworkComparisonValue
	}{
		{name: "fractional percentage stays unrounded", args: args{value: query.ReworkComparisonValue{Availability: "available", Numerator: proto.Float64(5004), Denominator: proto.Float64(10000), Value: proto.Float64(50.04)}}, want: &v1.ReworkComparisonValue{Availability: "available", Numerator: proto.Float64(5004), Denominator: proto.Float64(10000), Value: proto.Float64(50.04)}},
		{name: "missing numerator retains denominator and reason", args: args{value: query.ReworkComparisonValue{Availability: "unavailable", Reason: "numerator unavailable", Denominator: proto.Float64(1000)}}, want: &v1.ReworkComparisonValue{Availability: "unavailable", Reason: "numerator unavailable", Denominator: proto.Float64(1000)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapReworkComparisonValue(tt.args.value); !cmp.Equal(tt.want, got, protocmp.Transform()) {
				t.Errorf("mapReworkComparisonValue() = %v, want %v\ndiff=%s", got, tt.want, cmp.Diff(tt.want, got, protocmp.Transform()))
			}
		})
	}
}
