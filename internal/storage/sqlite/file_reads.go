package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/kotokumu/agentmetry/internal/query"
)

func (store *Store) ListSessionFileReads(ctx context.Context, filter query.SessionFileReadFilter) (query.SessionFileReadPage, error) {
	transaction, err := store.readDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return query.SessionFileReadPage{}, fmt.Errorf("begin session file read snapshot: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	ref := sessionRef{sourceID: filter.Identity.SourceID(), sessionID: filter.Identity.ConversationID()}
	graph, err := loadSessionGroupWithReader(ctx, transaction, ref)
	if err != nil {
		return query.SessionFileReadPage{}, err
	}
	members := graph.members(graph.root(ref))
	memberIDs := make([]string, len(members))
	for index, member := range members {
		memberIDs[index] = member.sessionID
	}
	const activityBatchSize = 100
	activityOffset := 0
	sawActivity := false
	matchedCount := 0
	distinct := make(map[string]struct{})
	limit := filter.Page.Size()
	offset := filter.Page.Offset()
	pageReads := make([]query.SessionFileRead, 0, limit)
	hasMore := false
	referenceFilter := strings.TrimSpace(filter.Reference)
	for {
		activities, err := store.activitiesWindowWithReaderSessions(ctx, transaction, formatTime(time.Unix(0, 0)), activityBatchSize, activityOffset, ref.sourceID, memberIDs, true, "")
		if err != nil {
			return query.SessionFileReadPage{}, fmt.Errorf("load session file read activities: %w", err)
		}
		if len(activities) == 0 {
			break
		}
		sawActivity = true
		for index := range activities {
			activity := graph.normalizeActivityAgent(activities[index])
			for _, reference := range query.ReportedFileReferencesWithFields(activity) {
				if referenceFilter != "" && reference.Value != referenceFilter {
					continue
				}
				if fileReferenceIsRedacted(activity, reference.Fields) {
					continue
				}
				distinct[reference.Value] = struct{}{}
				if matchedCount < offset {
					matchedCount++
					continue
				}
				if len(pageReads) == limit {
					hasMore = true
					matchedCount++
					continue
				}
				activityEvidence := activityContentEvidence(activity)
				evidence := fileReadEvidence(activity, reference.Fields)
				pageReads = append(pageReads, query.SessionFileRead{
					ID:       query.SessionFileReadID(activity.Source, activity.RunID, activity.ID, reference.Value),
					SourceID: activity.Source, SessionID: activity.RunID, Reference: reference.Value, ActivityID: activity.ID,
					ObservedAt: activity.ObservedAt, AgentID: activity.AgentID, Model: activity.Model,
					OutputAvailability: fileReadOutputAvailability(activityEvidence), OutputMapping: query.FileMappingNotConfirmed,
					ContentEvidence: evidence,
				})
				matchedCount++
			}
		}
		activityOffset += len(activities)
		if len(activities) < activityBatchSize {
			break
		}
	}
	if !sawActivity {
		return query.SessionFileReadPage{}, query.ErrConversationNotFound
	}
	if offset > matchedCount {
		offset = matchedCount
	}
	nextOffset := filter.Page.NextOffset(len(pageReads))
	if offset != filter.Page.Offset() {
		nextOffset = offset
	}
	page := query.SessionFileReadPage{
		Reads: pageReads, DistinctReferenceCount: int64(len(distinct)), NextOffset: nextOffset,
		HasMore: hasMore, Coverage: query.FileCoverageComplete,
	}
	if err := transaction.Commit(); err != nil {
		return query.SessionFileReadPage{}, fmt.Errorf("commit session file read snapshot: %w", err)
	}
	return page, nil
}

func fileReadOutputAvailability(evidence query.ContentEvidence) string {
	switch evidence.Kind {
	case "response", "tool_output", "tool_input_output":
		switch evidence.Availability {
		case query.FileOutputAvailable, query.FileOutputRedacted, query.FileOutputNotReturned:
			return evidence.Availability
		}
	}
	return query.FileOutputNotReported
}

func activityContentEvidence(activity query.Activity) query.ContentEvidence {
	if activity.ContentEvidence != nil {
		return *activity.ContentEvidence
	}
	return query.DescribeActivityContent(activity)
}

func fileReadEvidence(activity query.Activity, referenceFields []string) query.ContentEvidence {
	evidence := activityContentEvidence(activity)
	related := fieldsOverlap(evidence.Fields, referenceFields)
	if len(referenceFields) > 0 && !related {
		evidence = query.ContentEvidence{
			Source: activity.Source, ActivityID: activity.ID, Signal: string(activity.Signal),
			Kind: "reference", Evidence: "reference", Availability: query.FileOutputNotReported,
		}
	}
	if evidence.Kind == "unknown" && evidence.Evidence == "unknown" && evidence.Availability == query.FileOutputNotReported && len(evidence.Fields) == 0 {
		evidence.Kind = "reference"
		evidence.Evidence = "reference"
	}
	fields := make([]string, 0, len(referenceFields)+len(evidence.Fields))
	for _, field := range append(append([]string(nil), referenceFields...), evidence.Fields...) {
		duplicate := false
		for _, existing := range fields {
			if existing == field {
				duplicate = true
				break
			}
		}
		if !duplicate {
			fields = append(fields, field)
		}
	}
	evidence.Fields = fields
	return evidence
}

func fieldsOverlap(left, right []string) bool {
	for _, first := range left {
		for _, second := range right {
			if first == second {
				return true
			}
		}
	}
	return false
}

func fileReferenceIsRedacted(activity query.Activity, referenceFields []string) bool {
	evidence := activityContentEvidence(activity)
	return evidence.Availability == query.FileOutputRedacted && fieldsOverlap(evidence.Fields, referenceFields)
}
