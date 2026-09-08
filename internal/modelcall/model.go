// Package modelcall owns stable billable-call identity and cost attribution.
package modelcall

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
)

type IdentityBasis int

const (
	IdentityUnspecified IdentityBasis = iota
	IdentityClaudeClientRequestID
	IdentityClaudeRequestID
	IdentityClaudeEventSequence
	IdentityJournalEvidenceFallback
)

type Alias struct {
	stringValue string
	bytesValue  []byte
}

func StringAlias(value string) Alias  { return Alias{stringValue: value} }
func JournalAlias(value []byte) Alias { return Alias{bytesValue: append([]byte(nil), value...)} }

func CallID(source, session string, basis IdentityBasis, alias Alias) string {
	var value bytes.Buffer
	value.WriteString("agentmetry-call-id-v1")
	value.WriteByte(0)
	writeBytes(&value, []byte(source))
	if session == "" {
		value.WriteByte(0)
	} else {
		value.WriteByte(1)
		writeBytes(&value, []byte(session))
	}
	switch basis {
	case IdentityClaudeClientRequestID:
		value.WriteByte(0x01)
		writeBytes(&value, []byte(alias.stringValue))
	case IdentityClaudeRequestID:
		value.WriteByte(0x02)
		writeBytes(&value, []byte(alias.stringValue))
	case IdentityClaudeEventSequence:
		value.WriteByte(0x03)
		var encoded [8]byte
		binary.BigEndian.PutUint64(encoded[:], parseSequence(alias.stringValue))
		value.Write(encoded[:])
	case IdentityJournalEvidenceFallback:
		value.WriteByte(0x04)
		writeBytes(&value, alias.bytesValue)
	default:
		value.WriteByte(0)
	}
	hash := sha256.Sum256(value.Bytes())
	return hex.EncodeToString(hash[:])
}

func EncodeJournalLocator(source, signal string, payloadSHA256 []byte, occurrence, recordOrdinal int64) ([]byte, error) {
	if len(payloadSHA256) != sha256.Size {
		return nil, fmt.Errorf("payload SHA-256 must be %d bytes", sha256.Size)
	}
	if occurrence < 0 || recordOrdinal < 0 {
		return nil, fmt.Errorf("journal locator ordinals must be non-negative")
	}
	var value bytes.Buffer
	value.WriteString("agentmetry-journal-locator-v1")
	value.WriteByte(0)
	writeBytes(&value, []byte(source))
	writeBytes(&value, []byte(signal))
	value.Write(payloadSHA256)
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(occurrence))
	value.Write(encoded[:])
	binary.BigEndian.PutUint64(encoded[:], uint64(recordOrdinal))
	value.Write(encoded[:])
	return value.Bytes(), nil
}

func writeBytes(target *bytes.Buffer, value []byte) {
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(value)))
	target.Write(size[:])
	target.Write(value)
}

func parseSequence(value string) uint64 {
	var result uint64
	for _, digit := range value {
		if digit < '0' || digit > '9' || result > (math.MaxUint64-uint64(digit-'0'))/10 {
			return 0
		}
		result = result*10 + uint64(digit-'0')
	}
	return result
}

type CostBasis int

const (
	CostUnspecified CostBasis = iota
	CostProviderReported
	CostRateCardEstimate
	CostUnavailable
)

type SummaryBasis int

const (
	SummaryUnspecified SummaryBasis = iota
	SummaryProviderReported
	SummaryRateCardEstimate
	SummaryMixed
	SummaryUnavailable
)

type Coverage int

const (
	CoverageUnspecified Coverage = iota
	CoverageComplete
	CoveragePartial
	CoverageUnavailable
)

type UnavailableReason int

const (
	ReasonUnspecified UnavailableReason = iota
	ReasonConflictingAuthoritativeEvidence
	ReasonInvalidProviderAmount
	ReasonMissingModel
	ReasonMissingOccurredAt
	ReasonMissingTokenUsage
	ReasonUnsupportedBillingMode
	ReasonUnsupportedUsageCondition
	ReasonRateNotFound
	ReasonRateConflict
	ReasonCalculationError
)

type AggregationError int

const (
	AggregateNoError AggregationError = iota
	AggregateCalculationError
)

type Attribution struct {
	Basis          CostBasis
	AmountMicroUSD *int64
	Reason         UnavailableReason
	RateEntryID    string
}

type ReasonCount struct {
	Reason UnavailableReason
	Count  int64
}

type CostSummary struct {
	AmountMicroUSD  *int64
	Basis           SummaryBasis
	Coverage        Coverage
	EligibleCalls   int64
	PricedCalls     int64
	UnpricedReasons []ReasonCount
	AggregateError  AggregationError
}

func SummarizeCost(calls map[string]Attribution) CostSummary {
	result := CostSummary{EligibleCalls: int64(len(calls)), Basis: SummaryUnavailable, Coverage: CoverageUnavailable}
	reasons := make(map[UnavailableReason]int64)
	var total int64
	provider, rate, overflow := false, false, false
	for _, call := range calls {
		if call.AmountMicroUSD != nil && (call.Basis == CostProviderReported || call.Basis == CostRateCardEstimate) {
			result.PricedCalls++
			provider = provider || call.Basis == CostProviderReported
			rate = rate || call.Basis == CostRateCardEstimate
			if *call.AmountMicroUSD < 0 || total > math.MaxInt64-*call.AmountMicroUSD {
				overflow = true
			} else {
				total += *call.AmountMicroUSD
			}
			continue
		}
		reason := call.Reason
		if reason == ReasonUnspecified {
			reason = ReasonCalculationError
		}
		reasons[reason]++
	}
	for reason, count := range reasons {
		result.UnpricedReasons = append(result.UnpricedReasons, ReasonCount{Reason: reason, Count: count})
	}
	sort.Slice(result.UnpricedReasons, func(i, j int) bool { return result.UnpricedReasons[i].Reason < result.UnpricedReasons[j].Reason })
	switch {
	case provider && rate:
		result.Basis = SummaryMixed
	case provider:
		result.Basis = SummaryProviderReported
	case rate:
		result.Basis = SummaryRateCardEstimate
	}
	if overflow {
		result.AggregateError = AggregateCalculationError
		return result
	}
	if result.PricedCalls > 0 {
		result.AmountMicroUSD = &total
		if result.PricedCalls == result.EligibleCalls {
			result.Coverage = CoverageComplete
		} else {
			result.Coverage = CoveragePartial
		}
	}
	return result
}
