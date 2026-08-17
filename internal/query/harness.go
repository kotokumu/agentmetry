package query

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/kotokumu/agentmetry/internal/harness"
)

type HarnessEvidenceCounts struct {
	EligibleRecords    int64
	ReportedRecords    int64
	UnreportedRecords  int64
	InvalidRecords     int64
	DistinctIdentities int64
}

type HarnessIdentity struct {
	Scope       string
	Fingerprint string
	Label       string
}

type ReportedIdentityEvidence struct {
	Identity HarnessIdentity
	Records  int64
	Labels   []string
}

type HarnessEvidenceFacts struct {
	Counts     HarnessEvidenceCounts
	Identities []ReportedIdentityEvidence
}

type HarnessContext interface {
	isHarnessContext()
	EvidenceCounts() HarnessEvidenceCounts
}

type HarnessClassification string

const (
	HarnessNoEligibleRecords HarnessClassification = "no_eligible_records"
	HarnessUnreported        HarnessClassification = "unreported"
	HarnessUniform           HarnessClassification = "uniform"
	HarnessMixed             HarnessClassification = "mixed"
	HarnessIncomplete        HarnessClassification = "incomplete"
	HarnessInvalid           HarnessClassification = "invalid"
)

type HarnessContextView struct {
	Classification HarnessClassification
	Counts         HarnessEvidenceCounts
	Identity       *HarnessIdentity
}

type noEligibleRecordsHarnessContext struct{ counts HarnessEvidenceCounts }
type unreportedHarnessContext struct{ counts HarnessEvidenceCounts }
type uniformHarnessContext struct {
	counts   HarnessEvidenceCounts
	identity HarnessIdentity
}
type mixedHarnessContext struct{ counts HarnessEvidenceCounts }
type incompleteHarnessContext struct{ counts HarnessEvidenceCounts }
type invalidHarnessContext struct{ counts HarnessEvidenceCounts }

func (noEligibleRecordsHarnessContext) isHarnessContext() {}
func (unreportedHarnessContext) isHarnessContext()        {}
func (uniformHarnessContext) isHarnessContext()           {}
func (mixedHarnessContext) isHarnessContext()             {}
func (incompleteHarnessContext) isHarnessContext()        {}
func (invalidHarnessContext) isHarnessContext()           {}

func (context noEligibleRecordsHarnessContext) EvidenceCounts() HarnessEvidenceCounts {
	return context.counts
}
func (context unreportedHarnessContext) EvidenceCounts() HarnessEvidenceCounts { return context.counts }
func (context uniformHarnessContext) EvidenceCounts() HarnessEvidenceCounts    { return context.counts }
func (context mixedHarnessContext) EvidenceCounts() HarnessEvidenceCounts      { return context.counts }
func (context incompleteHarnessContext) EvidenceCounts() HarnessEvidenceCounts { return context.counts }
func (context invalidHarnessContext) EvidenceCounts() HarnessEvidenceCounts    { return context.counts }

func InspectHarnessContext(value HarnessContext) (HarnessContextView, error) {
	if value == nil {
		return HarnessContextView{}, fmt.Errorf("harness context is nil")
	}
	counts := value.EvidenceCounts()
	if err := validateHarnessCounts(counts); err != nil {
		return HarnessContextView{}, err
	}
	view := HarnessContextView{Counts: counts}
	switch context := value.(type) {
	case noEligibleRecordsHarnessContext:
		view.Classification = HarnessNoEligibleRecords
		if counts != (HarnessEvidenceCounts{}) {
			return HarnessContextView{}, fmt.Errorf("no-eligible harness context has evidence")
		}
	case unreportedHarnessContext:
		view.Classification = HarnessUnreported
		if counts.EligibleRecords == 0 || counts.UnreportedRecords != counts.EligibleRecords || counts.ReportedRecords != 0 || counts.InvalidRecords != 0 || counts.DistinctIdentities != 0 {
			return HarnessContextView{}, fmt.Errorf("unreported harness context is inconsistent")
		}
	case uniformHarnessContext:
		view.Classification = HarnessUniform
		if counts.EligibleRecords == 0 || counts.ReportedRecords != counts.EligibleRecords || counts.UnreportedRecords != 0 || counts.InvalidRecords != 0 || counts.DistinctIdentities != 1 {
			return HarnessContextView{}, fmt.Errorf("uniform harness context counts are inconsistent")
		}
		if !harness.ValidScope(context.identity.Scope) || !harness.ValidFingerprint(context.identity.Fingerprint) || !harness.ValidLabel(context.identity.Label) {
			return HarnessContextView{}, fmt.Errorf("uniform harness identity is invalid")
		}
		identity := context.identity
		view.Identity = &identity
	case mixedHarnessContext:
		view.Classification = HarnessMixed
		if counts.EligibleRecords == 0 || counts.ReportedRecords != counts.EligibleRecords || counts.UnreportedRecords != 0 || counts.InvalidRecords != 0 || counts.DistinctIdentities <= 1 {
			return HarnessContextView{}, fmt.Errorf("mixed harness context is inconsistent")
		}
	case incompleteHarnessContext:
		view.Classification = HarnessIncomplete
		if counts.EligibleRecords == 0 || counts.ReportedRecords == 0 || counts.UnreportedRecords == 0 || counts.InvalidRecords != 0 || counts.DistinctIdentities == 0 {
			return HarnessContextView{}, fmt.Errorf("incomplete harness context is inconsistent")
		}
	case invalidHarnessContext:
		view.Classification = HarnessInvalid
		if counts.EligibleRecords == 0 || counts.InvalidRecords == 0 {
			return HarnessContextView{}, fmt.Errorf("invalid harness context is inconsistent")
		}
	default:
		return HarnessContextView{}, fmt.Errorf("unsupported harness context %T", value)
	}
	return view, nil
}

func validateHarnessCounts(counts HarnessEvidenceCounts) error {
	if counts.EligibleRecords < 0 || counts.ReportedRecords < 0 || counts.UnreportedRecords < 0 || counts.InvalidRecords < 0 || counts.DistinctIdentities < 0 {
		return fmt.Errorf("harness evidence counts must be non-negative")
	}
	if counts.EligibleRecords != counts.ReportedRecords+counts.UnreportedRecords+counts.InvalidRecords {
		return fmt.Errorf("harness evidence counts do not sum to eligible records")
	}
	if counts.DistinctIdentities > counts.ReportedRecords {
		return fmt.Errorf("harness distinct identity count is inconsistent")
	}
	return nil
}

func ClassifyHarnessEvidence(facts HarnessEvidenceFacts) (HarnessContext, error) {
	counts := facts.Counts
	if err := validateHarnessCounts(counts); err != nil {
		return nil, err
	}
	if counts.DistinctIdentities != int64(len(facts.Identities)) {
		return nil, fmt.Errorf("harness distinct identity count is inconsistent")
	}
	seen := make(map[string]struct{}, len(facts.Identities))
	var identityRecords int64
	for _, evidence := range facts.Identities {
		if evidence.Records <= 0 {
			return nil, fmt.Errorf("harness identity record count must be positive")
		}
		if evidence.Identity.Label != "" || !harness.ValidScope(evidence.Identity.Scope) || !harness.ValidFingerprint(evidence.Identity.Fingerprint) {
			return nil, fmt.Errorf("harness identity is invalid")
		}
		key := evidence.Identity.Scope + "\x00" + evidence.Identity.Fingerprint
		if _, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf("duplicate harness identity evidence")
		}
		seen[key] = struct{}{}
		for _, label := range evidence.Labels {
			if strings.TrimSpace(label) != label || !harness.ValidLabel(label) {
				return nil, fmt.Errorf("harness label is invalid")
			}
		}
		identityRecords += evidence.Records
	}
	if identityRecords != counts.ReportedRecords {
		return nil, fmt.Errorf("harness identity records do not sum to reported records")
	}
	if counts.EligibleRecords == 0 {
		return noEligibleRecordsHarnessContext{counts: counts}, nil
	}
	if counts.InvalidRecords > 0 {
		return invalidHarnessContext{counts: counts}, nil
	}
	if counts.ReportedRecords == 0 {
		return unreportedHarnessContext{counts: counts}, nil
	}
	if counts.UnreportedRecords > 0 {
		return incompleteHarnessContext{counts: counts}, nil
	}
	if len(facts.Identities) > 1 {
		return mixedHarnessContext{counts: counts}, nil
	}
	evidence := facts.Identities[0]
	identity := evidence.Identity
	for _, label := range evidence.Labels {
		if label != "" && (identity.Label == "" || bytes.Compare([]byte(label), []byte(identity.Label)) < 0) {
			identity.Label = label
		}
	}
	return uniformHarnessContext{counts: counts, identity: identity}, nil
}
