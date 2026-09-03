package query

import (
	"math"
	"strings"
	"testing"
)

func TestValidateSessionConditions(t *testing.T) {
	type args struct {
		conditions SessionConditions
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{name: "default preserves existing query", args: args{conditions: SessionConditions{}}},
		{name: "inclusive equal zero bounds", args: args{conditions: SessionConditions{ObservedFailure: true, MinDurationMS: new(float64(0)), MaxDurationMS: new(float64(0)), Model: "gpt-5", Tool: "shell"}}},
		{name: "fractional milliseconds", args: args{conditions: SessionConditions{MinDurationMS: new(0.25), MaxDurationMS: new(10.5)}}},
		{name: "negative minimum", args: args{conditions: SessionConditions{MinDurationMS: new(-1.0)}}, wantErr: true},
		{name: "negative maximum", args: args{conditions: SessionConditions{MaxDurationMS: new(-1.0)}}, wantErr: true},
		{name: "inverted range", args: args{conditions: SessionConditions{MinDurationMS: new(2.0), MaxDurationMS: new(1.0)}}, wantErr: true},
		{name: "nan minimum", args: args{conditions: SessionConditions{MinDurationMS: new(math.NaN())}}, wantErr: true},
		{name: "positive infinite maximum", args: args{conditions: SessionConditions{MaxDurationMS: new(math.Inf(1))}}, wantErr: true},
		{name: "negative infinite minimum", args: args{conditions: SessionConditions{MinDurationMS: new(math.Inf(-1))}}, wantErr: true},
		{name: "bounded model", args: args{conditions: SessionConditions{Model: strings.Repeat("m", 201)}}, wantErr: true},
		{name: "bounded tool", args: args{conditions: SessionConditions{Tool: strings.Repeat("t", 201)}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateSessionConditions(tt.args.conditions); (err != nil) != tt.wantErr {
				t.Errorf("ValidateSessionConditions() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
