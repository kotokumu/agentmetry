package sqlite

import (
	"context"
	"fmt"
	"sort"

	"github.com/theoden9014/agentmetry/internal/query"
)

func (store *Store) loadSessionHarnessContext(ctx context.Context, reader sqlReader, root sessionRef, graph sessionGraph) (query.HarnessContext, error) {
	facts, err := store.loadSessionHarnessEvidenceFacts(ctx, reader, root, graph)
	if err != nil {
		return nil, err
	}
	context, err := query.ClassifyHarnessEvidence(facts)
	if err != nil {
		return nil, fmt.Errorf("classify session harness evidence: %w", err)
	}
	return context, nil
}

func (store *Store) loadSessionHarnessEvidenceFacts(ctx context.Context, reader sqlReader, root sessionRef, graph sessionGraph) (query.HarnessEvidenceFacts, error) {
	members := graph.members(root)
	predicate, memberArgs := sessionMembershipPredicate("observations.session_id", members)
	statement := fmt.Sprintf(`SELECT otlp_exports.harness_receipt_state,
  otlp_exports.harness_scope, otlp_exports.harness_fingerprint,
  otlp_exports.harness_label, COUNT(*)
FROM observations
JOIN otlp_exports ON otlp_exports.id = observations.export_id
WHERE observations.source = ? AND %s
  AND observations.kind <> 'unknown'
  AND observations.signal IN ('trace', 'log')
GROUP BY otlp_exports.harness_receipt_state, otlp_exports.harness_scope,
  otlp_exports.harness_fingerprint, otlp_exports.harness_label
ORDER BY otlp_exports.harness_receipt_state, otlp_exports.harness_scope,
  otlp_exports.harness_fingerprint, otlp_exports.harness_label`, predicate)
	args := append([]any{root.sourceID}, memberArgs...)
	rows, err := reader.QueryContext(ctx, statement, args...)
	if err != nil {
		return query.HarnessEvidenceFacts{}, fmt.Errorf("query session harness evidence: %w", err)
	}
	defer rows.Close()
	type identityKey struct{ scope, fingerprint string }
	type identityAggregate struct {
		records int64
		labels  map[string]struct{}
	}
	identities := make(map[identityKey]*identityAggregate)
	var facts query.HarnessEvidenceFacts
	for rows.Next() {
		var state, scope, fingerprint, label string
		var records int64
		if err := rows.Scan(&state, &scope, &fingerprint, &label, &records); err != nil {
			return query.HarnessEvidenceFacts{}, fmt.Errorf("scan session harness evidence: %w", err)
		}
		facts.Counts.EligibleRecords += records
		switch state {
		case "unreported":
			facts.Counts.UnreportedRecords += records
		case "invalid":
			facts.Counts.InvalidRecords += records
		case "reported":
			facts.Counts.ReportedRecords += records
			key := identityKey{scope: scope, fingerprint: fingerprint}
			aggregate := identities[key]
			if aggregate == nil {
				aggregate = &identityAggregate{labels: make(map[string]struct{})}
				identities[key] = aggregate
			}
			aggregate.records += records
			aggregate.labels[label] = struct{}{}
		default:
			return query.HarnessEvidenceFacts{}, fmt.Errorf("unknown harness receipt state %q", state)
		}
	}
	if err := rows.Err(); err != nil {
		return query.HarnessEvidenceFacts{}, fmt.Errorf("iterate session harness evidence: %w", err)
	}
	keys := make([]identityKey, 0, len(identities))
	for key := range identities {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool {
		if keys[left].scope != keys[right].scope {
			return keys[left].scope < keys[right].scope
		}
		return keys[left].fingerprint < keys[right].fingerprint
	})
	for _, key := range keys {
		aggregate := identities[key]
		labels := make([]string, 0, len(aggregate.labels))
		for label := range aggregate.labels {
			labels = append(labels, label)
		}
		sort.Strings(labels)
		facts.Identities = append(facts.Identities, query.ReportedIdentityEvidence{
			Identity: query.HarnessIdentity{Scope: key.scope, Fingerprint: key.fingerprint},
			Records:  aggregate.records, Labels: labels,
		})
	}
	facts.Counts.DistinctIdentities = int64(len(facts.Identities))
	return facts, nil
}
