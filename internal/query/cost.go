package query

type ModelCallCost struct {
	CallID         string `json:"callId"`
	IdentityBasis  string `json:"identityBasis"`
	Basis          string `json:"basis"`
	AmountMicroUSD *int64 `json:"amountMicroUsd,omitempty"`
	PrimaryReason  string `json:"primaryReason,omitempty"`
	RateEntryID    string `json:"rateEntryId,omitempty"`
}

type ModelCallRef struct {
	CallID        string `json:"callId"`
	IdentityBasis string `json:"identityBasis"`
	EvidenceRole  string `json:"evidenceRole"`
}

type CostReasonCount struct {
	Reason string `json:"reason"`
	Count  int64  `json:"count"`
}

type CostSummary struct {
	AmountMicroUSD  *int64            `json:"amountMicroUsd,omitempty"`
	Basis           string            `json:"basis"`
	Coverage        string            `json:"coverage"`
	EligibleCalls   int64             `json:"eligibleCalls"`
	PricedCalls     int64             `json:"pricedCalls"`
	UnpricedReasons []CostReasonCount `json:"unpricedReasons"`
	AggregateError  string            `json:"aggregateError,omitempty"`
}
