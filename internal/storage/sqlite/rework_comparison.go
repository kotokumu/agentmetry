package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/kotokumu/agentmetry/internal/query"
)

func (store *Store) CompareRework(ctx context.Context, pair query.ReworkComparisonPair) (query.ReworkComparison, error) {
	transaction, err := store.readDB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return query.ReworkComparison{}, fmt.Errorf("begin rework comparison snapshot: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	baseline, err := store.loadReworkDiagnosticSnapshot(ctx, transaction, pair.Baseline)
	if err != nil {
		return query.ReworkComparison{}, err
	}
	current, err := store.loadReworkDiagnosticSnapshot(ctx, transaction, pair.Current)
	if err != nil {
		return query.ReworkComparison{}, err
	}
	comparison := query.CompareReworkSnapshots(baseline, current)
	if err := transaction.Commit(); err != nil {
		return query.ReworkComparison{}, fmt.Errorf("commit rework comparison snapshot: %w", err)
	}
	return comparison, nil
}

func (store *Store) loadReworkDiagnosticSnapshot(ctx context.Context, reader sqlReader, identity query.ConversationIdentity) (query.ReworkDiagnosticSnapshot, error) {
	ref := sessionRef{sourceID: identity.SourceID(), sessionID: identity.ConversationID()}
	graph, err := loadSessionGroupWithReader(ctx, reader, ref)
	if err != nil {
		return query.ReworkDiagnosticSnapshot{}, err
	}
	root := graph.root(ref)
	summary, err := store.loadSessionSummary(ctx, reader, root, graph)
	if err != nil {
		return query.ReworkDiagnosticSnapshot{}, err
	}
	activities, err := store.loadSessionReworkActivities(ctx, reader, root, graph)
	if err != nil {
		return query.ReworkDiagnosticSnapshot{}, err
	}
	harnessContext, err := store.loadSessionHarnessContext(ctx, reader, root, graph)
	if err != nil {
		return query.ReworkDiagnosticSnapshot{}, err
	}
	resolved, err := query.NewConversationIdentity(root.sourceID, root.sessionID)
	if err != nil {
		return query.ReworkDiagnosticSnapshot{}, err
	}
	return query.ReworkDiagnosticSnapshot{
		Identity: resolved, StartedAt: summary.StartedAt, EndedAt: summary.EndedAt,
		Analysis: query.SessionRework{
			SourceID: root.sourceID, RunID: root.sessionID, SessionTokens: summary.Tokens,
			Harness: harnessContext, Report: query.AnalyzeRework(summary, activities),
		},
	}, nil
}
