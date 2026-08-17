package harness

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseMetadata(t *testing.T) {
	type args struct {
		values MetadataValues
	}
	tests := []struct {
		name string
		args args
		want ReceiptEvidence
	}{
		{name: "absent metadata", args: args{values: MetadataValues{}}, want: ReceiptEvidence{State: ReceiptUnreported}},
		{name: "all empty metadata", args: args{values: MetadataValues{Scope: []string{""}, Fingerprint: []string{""}, Label: []string{""}}}, want: ReceiptEvidence{State: ReceiptUnreported}},
		{name: "reported identity", args: args{values: MetadataValues{Scope: []string{"project-7f2a"}, Fingerprint: []string{"sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db"}}}, want: ReceiptEvidence{State: ReceiptReported, Scope: "project-7f2a", Fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db"}},
		{name: "reported identity trims label", args: args{values: MetadataValues{Scope: []string{"project-7f2a"}, Fingerprint: []string{"sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db"}, Label: []string{"  AGENTS v2  "}}}, want: ReceiptEvidence{State: ReceiptReported, Scope: "project-7f2a", Fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db", Label: "AGENTS v2"}},
		{name: "partial metadata", args: args{values: MetadataValues{Scope: []string{"project-7f2a"}}}, want: ReceiptEvidence{State: ReceiptInvalid}},
		{name: "invalid scope", args: args{values: MetadataValues{Scope: []string{"project space"}, Fingerprint: []string{"sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db"}}}, want: ReceiptEvidence{State: ReceiptInvalid}},
		{name: "invalid fingerprint", args: args{values: MetadataValues{Scope: []string{"project-7f2a"}, Fingerprint: []string{"sha256:ABC"}}}, want: ReceiptEvidence{State: ReceiptInvalid}},
		{name: "control character in label", args: args{values: MetadataValues{Scope: []string{"project-7f2a"}, Fingerprint: []string{"sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db"}, Label: []string{"bad\nlabel"}}}, want: ReceiptEvidence{State: ReceiptInvalid}},
		{name: "label over eighty code points", args: args{values: MetadataValues{Scope: []string{"project-7f2a"}, Fingerprint: []string{"sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db"}, Label: []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}, want: ReceiptEvidence{State: ReceiptInvalid}},
		{name: "duplicate equal scope", args: args{values: MetadataValues{Scope: []string{"project-7f2a", "project-7f2a"}, Fingerprint: []string{"sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db"}}}, want: ReceiptEvidence{State: ReceiptInvalid}},
		{name: "duplicate conflicting fingerprint", args: args{values: MetadataValues{Scope: []string{"project-7f2a"}, Fingerprint: []string{"sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db", "sha256:dfbc1de58f3b905c7b0c0fd79361699336b5f9da617b1db8f35c76673f95b29d"}}}, want: ReceiptEvidence{State: ReceiptInvalid}},
		{name: "comma joined value", args: args{values: MetadataValues{Scope: []string{"project-7f2a"}, Fingerprint: []string{"sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db"}, Label: []string{"before,after"}}}, want: ReceiptEvidence{State: ReceiptInvalid}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseMetadata(tt.args.values); !cmp.Equal(tt.want, got) {
				t.Errorf("ParseMetadata() = %v, want %v\ndiff=%s", got, tt.want, cmp.Diff(tt.want, got))
			}
		})
	}
}
