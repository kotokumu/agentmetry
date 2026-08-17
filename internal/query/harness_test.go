package query

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestClassifyHarnessEvidence(t *testing.T) {
	type args struct {
		facts HarnessEvidenceFacts
	}
	tests := []struct {
		name    string
		args    args
		want    HarnessContext
		wantErr bool
	}{
		{name: "no eligible records", args: args{facts: HarnessEvidenceFacts{}}, want: noEligibleRecordsHarnessContext{}},
		{name: "unreported", args: args{facts: HarnessEvidenceFacts{Counts: HarnessEvidenceCounts{EligibleRecords: 2, UnreportedRecords: 2}}}, want: unreportedHarnessContext{counts: HarnessEvidenceCounts{EligibleRecords: 2, UnreportedRecords: 2}}},
		{
			name: "uniform chooses smallest label by UTF-8 bytes",
			args: args{facts: HarnessEvidenceFacts{
				Counts:     HarnessEvidenceCounts{EligibleRecords: 2, ReportedRecords: 2, DistinctIdentities: 1},
				Identities: []ReportedIdentityEvidence{{Identity: HarnessIdentity{Scope: "project-7f2a", Fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db"}, Records: 2, Labels: []string{"é", "_a", "😀", "", "A"}}},
			}},
			want: uniformHarnessContext{counts: HarnessEvidenceCounts{EligibleRecords: 2, ReportedRecords: 2, DistinctIdentities: 1}, identity: HarnessIdentity{Scope: "project-7f2a", Fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db", Label: "A"}},
		},
		{
			name: "mixed",
			args: args{facts: HarnessEvidenceFacts{
				Counts: HarnessEvidenceCounts{EligibleRecords: 2, ReportedRecords: 2, DistinctIdentities: 2},
				Identities: []ReportedIdentityEvidence{
					{Identity: HarnessIdentity{Scope: "project-7f2a", Fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db"}, Records: 1},
					{Identity: HarnessIdentity{Scope: "project-7f2a", Fingerprint: "sha256:dfbc1de58f3b905c7b0c0fd79361699336b5f9da617b1db8f35c76673f95b29d"}, Records: 1},
				},
			}},
			want: mixedHarnessContext{counts: HarnessEvidenceCounts{EligibleRecords: 2, ReportedRecords: 2, DistinctIdentities: 2}},
		},
		{
			name: "incomplete",
			args: args{facts: HarnessEvidenceFacts{
				Counts:     HarnessEvidenceCounts{EligibleRecords: 2, ReportedRecords: 1, UnreportedRecords: 1, DistinctIdentities: 1},
				Identities: []ReportedIdentityEvidence{{Identity: HarnessIdentity{Scope: "project-7f2a", Fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db"}, Records: 1}},
			}},
			want: incompleteHarnessContext{counts: HarnessEvidenceCounts{EligibleRecords: 2, ReportedRecords: 1, UnreportedRecords: 1, DistinctIdentities: 1}},
		},
		{
			name: "invalid takes precedence",
			args: args{facts: HarnessEvidenceFacts{
				Counts:     HarnessEvidenceCounts{EligibleRecords: 3, ReportedRecords: 1, UnreportedRecords: 1, InvalidRecords: 1, DistinctIdentities: 1},
				Identities: []ReportedIdentityEvidence{{Identity: HarnessIdentity{Scope: "project-7f2a", Fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db"}, Records: 1}},
			}},
			want: invalidHarnessContext{counts: HarnessEvidenceCounts{EligibleRecords: 3, ReportedRecords: 1, UnreportedRecords: 1, InvalidRecords: 1, DistinctIdentities: 1}},
		},
		{name: "rejects negative count", args: args{facts: HarnessEvidenceFacts{Counts: HarnessEvidenceCounts{EligibleRecords: -1}}}, wantErr: true},
		{name: "rejects count equation mismatch", args: args{facts: HarnessEvidenceFacts{Counts: HarnessEvidenceCounts{EligibleRecords: 2, UnreportedRecords: 1}}}, wantErr: true},
		{name: "rejects distinct count without reported records", args: args{facts: HarnessEvidenceFacts{Counts: HarnessEvidenceCounts{EligibleRecords: 1, UnreportedRecords: 1, DistinctIdentities: 1}, Identities: []ReportedIdentityEvidence{{Identity: HarnessIdentity{Scope: "project-7f2a", Fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db"}, Records: 1}}}}, wantErr: true},
		{name: "rejects identity list mismatch", args: args{facts: HarnessEvidenceFacts{Counts: HarnessEvidenceCounts{EligibleRecords: 1, ReportedRecords: 1, DistinctIdentities: 1}}}, wantErr: true},
		{
			name: "rejects duplicate identity",
			args: args{facts: HarnessEvidenceFacts{
				Counts: HarnessEvidenceCounts{EligibleRecords: 2, ReportedRecords: 2, DistinctIdentities: 2},
				Identities: []ReportedIdentityEvidence{
					{Identity: HarnessIdentity{Scope: "project-7f2a", Fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db"}, Records: 1},
					{Identity: HarnessIdentity{Scope: "project-7f2a", Fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db"}, Records: 1},
				},
			}}, wantErr: true,
		},
		{name: "rejects invalid identity", args: args{facts: HarnessEvidenceFacts{Counts: HarnessEvidenceCounts{EligibleRecords: 1, ReportedRecords: 1, DistinctIdentities: 1}, Identities: []ReportedIdentityEvidence{{Identity: HarnessIdentity{Scope: "project space", Fingerprint: "sha256:bad"}, Records: 1}}}}, wantErr: true},
		{name: "rejects non-positive identity records", args: args{facts: HarnessEvidenceFacts{Counts: HarnessEvidenceCounts{EligibleRecords: 1, ReportedRecords: 1, DistinctIdentities: 1}, Identities: []ReportedIdentityEvidence{{Identity: HarnessIdentity{Scope: "project-7f2a", Fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db"}, Records: 0}}}}, wantErr: true},
		{name: "rejects identity record sum mismatch", args: args{facts: HarnessEvidenceFacts{Counts: HarnessEvidenceCounts{EligibleRecords: 2, ReportedRecords: 2, DistinctIdentities: 1}, Identities: []ReportedIdentityEvidence{{Identity: HarnessIdentity{Scope: "project-7f2a", Fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db"}, Records: 1}}}}, wantErr: true},
		{name: "rejects invalid label", args: args{facts: HarnessEvidenceFacts{Counts: HarnessEvidenceCounts{EligibleRecords: 1, ReportedRecords: 1, DistinctIdentities: 1}, Identities: []ReportedIdentityEvidence{{Identity: HarnessIdentity{Scope: "project-7f2a", Fingerprint: "sha256:8643ebd621ce63157c7bdeaef885ab93885202e45a4ae7c185c4c7b42bb839db"}, Records: 1, Labels: []string{"bad,label"}}}}}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ClassifyHarnessEvidence(tt.args.facts)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ClassifyHarnessEvidence() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			gotView, gotErr := InspectHarnessContext(got)
			wantView, wantErr := InspectHarnessContext(tt.want)
			if gotErr != nil || wantErr != nil || !cmp.Equal(wantView, gotView) {
				t.Errorf("ClassifyHarnessEvidence() = %v (%v), want %v (%v)\ndiff=%s", gotView, gotErr, wantView, wantErr, cmp.Diff(wantView, gotView))
			}
		})
	}
}
