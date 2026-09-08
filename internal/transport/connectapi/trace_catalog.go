package connectapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"

	v1 "github.com/kotokumu/agentmetry/gen/agentmetry/v1"
	"github.com/kotokumu/agentmetry/internal/query"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	traceListPageTokenKind = "trace-list"
	fileReadsPageTokenKind = "session-file-reads"
	pageTokenPrefix        = "agentmetry:v1:"
)

func encodeBoundPageToken(kind string, offset int, binding string) string {
	digest := sha256.Sum256([]byte(binding))
	payload := fmt.Sprintf("%s%s:%d:%x", pageTokenPrefix, kind, offset, digest[:12])
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func parseBoundPageToken(token, kind, binding string) (int, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0, fmt.Errorf("invalid page_token")
	}
	parts := strings.Split(string(decoded), ":")
	if len(parts) != 5 || parts[0]+":"+parts[1]+":" != pageTokenPrefix || parts[2] != kind {
		return 0, fmt.Errorf("invalid page_token")
	}
	offset, err := strconv.Atoi(parts[3])
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("invalid page_token")
	}
	digest := sha256.Sum256([]byte(binding))
	provided, err := hex.DecodeString(parts[4])
	if err != nil || len(provided) != 12 || subtle.ConstantTimeCompare(provided, digest[:12]) != 1 {
		return 0, fmt.Errorf("page_token does not match the request")
	}
	return offset, nil
}

func traceListPageBinding(filter query.TraceListFilter, pageSize int, timeRange v1.TimeRange) string {
	minDuration := "nil"
	if filter.Conditions.MinDurationMS != nil {
		minDuration = strconv.FormatFloat(*filter.Conditions.MinDurationMS, 'g', -1, 64)
	}
	return strings.Join([]string{
		strconv.Itoa(int(timeRange)), filter.SourceID,
		string(filter.Conditions.FailureObservation), minDuration, strconv.Itoa(pageSize),
	}, "\x00")
}

func fileReadsPageBinding(identity query.ConversationIdentity, reference string, pageSize int) string {
	return strings.Join([]string{identity.SourceID(), identity.ConversationID(), reference, strconv.Itoa(pageSize)}, "\x00")
}

func traceConditions(input *v1.TraceConditions) (query.TraceConditions, error) {
	if input == nil {
		return query.TraceConditions{}, nil
	}
	conditions := query.TraceConditions{MinDurationMS: input.MinDurationMs}
	if input.MinDurationMs != nil && (math.IsNaN(*input.MinDurationMs) || math.IsInf(*input.MinDurationMs, 0) || *input.MinDurationMs < 0) {
		return query.TraceConditions{}, fmt.Errorf("min duration must be finite and non-negative")
	}
	switch input.GetFailureObservation() {
	case v1.TraceFailureObservation_TRACE_FAILURE_OBSERVATION_UNSPECIFIED:
		conditions.FailureObservation = query.TraceFailureUnspecified
	case v1.TraceFailureObservation_TRACE_FAILURE_OBSERVATION_OBSERVED:
		conditions.FailureObservation = query.TraceFailureObserved
	case v1.TraceFailureObservation_TRACE_FAILURE_OBSERVATION_NOT_OBSERVED:
		conditions.FailureObservation = query.TraceFailureNotObserved
	case v1.TraceFailureObservation_TRACE_FAILURE_OBSERVATION_NOT_REPORTED:
		conditions.FailureObservation = query.TraceFailureNotReported
	default:
		return query.TraceConditions{}, fmt.Errorf("unsupported trace failure observation %q", input.GetFailureObservation())
	}
	return conditions, nil
}

func mapTraceConditions(input *query.TraceConditions) *v1.TraceConditions {
	if input == nil {
		return nil
	}
	result := &v1.TraceConditions{MinDurationMs: input.MinDurationMS}
	switch input.FailureObservation {
	case query.TraceFailureObserved:
		result.FailureObservation = v1.TraceFailureObservation_TRACE_FAILURE_OBSERVATION_OBSERVED
	case query.TraceFailureNotObserved:
		result.FailureObservation = v1.TraceFailureObservation_TRACE_FAILURE_OBSERVATION_NOT_OBSERVED
	case query.TraceFailureNotReported:
		result.FailureObservation = v1.TraceFailureObservation_TRACE_FAILURE_OBSERVATION_NOT_REPORTED
	default:
		result.FailureObservation = v1.TraceFailureObservation_TRACE_FAILURE_OBSERVATION_UNSPECIFIED
	}
	return result
}

func mapTraceSummaries(values []query.TraceListEntry) []*v1.TraceSummary {
	result := make([]*v1.TraceSummary, 0, len(values))
	for _, value := range values {
		entry := &v1.TraceSummary{
			TraceId: value.TraceID, Status: string(value.Status), ActivityCount: value.ActivityCount,
			RootSpanCount: value.RootSpanCount, MissingParentCount: value.MissingParentCount, CostSummary: mapCostSummary(value.CostSummary),
		}
		if value.StartedAt != nil {
			entry.StartedAt = timestamppb.New(*value.StartedAt)
		}
		if value.EndedAt != nil {
			entry.EndedAt = timestamppb.New(*value.EndedAt)
		}
		entry.DurationMs = value.DurationMS
		for _, conversation := range value.Conversations {
			entry.Conversations = append(entry.Conversations, &v1.ConversationRef{SourceId: conversation.SourceID, Id: conversation.ID})
		}
		result = append(result, entry)
	}
	return result
}

func mapSessionFileReads(values []query.SessionFileRead) []*v1.SessionFileRead {
	result := make([]*v1.SessionFileRead, 0, len(values))
	for _, value := range values {
		result = append(result, &v1.SessionFileRead{
			Id: value.ID, SourceId: value.SourceID, SessionId: value.SessionID, Reference: value.Reference,
			ActivityId: value.ActivityID, ObservedAt: timestamppb.New(value.ObservedAt), AgentId: value.AgentID, Model: value.Model,
			OutputContent: value.OutputContent, OutputAvailability: value.OutputAvailability, OutputMapping: value.OutputMapping,
			ContentEvidence: &v1.ContentEvidence{Source: value.ContentEvidence.Source, ActivityId: value.ContentEvidence.ActivityID, Signal: value.ContentEvidence.Signal,
				Kind: value.ContentEvidence.Kind, Evidence: value.ContentEvidence.Evidence, Availability: value.ContentEvidence.Availability,
				Fields: value.ContentEvidence.Fields, Truncated: value.ContentEvidence.Truncated, RedactionReason: value.ContentEvidence.RedactionReason},
		})
	}
	return result
}
