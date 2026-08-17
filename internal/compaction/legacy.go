package compaction

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/theoden9014/agentmetry/internal/canonical"
	"github.com/theoden9014/agentmetry/internal/harness"
	"github.com/theoden9014/agentmetry/internal/ingest"
	"github.com/theoden9014/agentmetry/internal/journal"
)

type storedExport struct {
	Ordinal            int64
	ReceivedAt         time.Time
	Signal             canonical.Signal
	Transport          ingest.Transport
	Stored             []byte
	Codec              journal.Codec
	Size               int
	Hash               [sha256.Size]byte
	Metadata           ingest.JournalMetadata
	NormalizationError string
}

type legacyReader struct {
	rows  *sql.Rows
	total int64
	next  int64
}

func newLegacyReader(ctx context.Context, transaction *sql.Tx) (*legacyReader, error) {
	var total int64
	if err := transaction.QueryRowContext(ctx, "SELECT COUNT(*) FROM otlp_exports").Scan(&total); err != nil {
		return nil, fmt.Errorf("count legacy exports: %w", err)
	}
	hasCodec, err := columnExistsTx(ctx, transaction, "otlp_exports", "payload_codec")
	if err != nil {
		return nil, err
	}
	codecExpression := "'identity'"
	if hasCodec {
		codecExpression = "payload_codec"
	}
	harnessExpressions := []string{"'unreported'", "''", "''", "''"}
	for index, column := range []string{"harness_receipt_state", "harness_scope", "harness_fingerprint", "harness_label"} {
		exists, err := columnExistsTx(ctx, transaction, "otlp_exports", column)
		if err != nil {
			return nil, err
		}
		if exists {
			harnessExpressions[index] = column
		}
	}
	rows, err := transaction.QueryContext(ctx, `SELECT received_at, signal, transport,
payload_protobuf, `+codecExpression+`, payload_size, payload_sha256, source,
normalizer_version, normalization_status, normalization_error, `+
		harnessExpressions[0]+`, `+harnessExpressions[1]+`, `+harnessExpressions[2]+`, `+harnessExpressions[3]+`
FROM otlp_exports ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("read legacy exports: %w", err)
	}
	return &legacyReader{rows: rows, total: total}, nil
}

func (reader *legacyReader) Total() int64 { return reader.total }
func (reader *legacyReader) Next() bool   { return reader.rows.Next() }

func (reader *legacyReader) Export() (storedExport, error) {
	var receivedText, signalText, transportText, codecText, hashText string
	var sourceID, status, normalizationError, harnessState, harnessScope, harnessFingerprint, harnessLabel string
	var stored []byte
	var originalSize, normalizerVersion int
	if err := reader.rows.Scan(
		&receivedText, &signalText, &transportText, &stored, &codecText,
		&originalSize, &hashText, &sourceID, &normalizerVersion, &status,
		&normalizationError, &harnessState, &harnessScope, &harnessFingerprint, &harnessLabel,
	); err != nil {
		return storedExport{}, fmt.Errorf("scan legacy export: %w", err)
	}
	reader.next++
	receivedAt, err := time.Parse(time.RFC3339Nano, receivedText)
	if err != nil {
		return storedExport{}, fmt.Errorf("parse legacy receive time: %w", err)
	}
	hash, err := parseHash(hashText)
	if err != nil {
		return storedExport{}, err
	}
	return storedExport{
		Ordinal: reader.next, ReceivedAt: receivedAt,
		Signal: canonical.Signal(signalText), Transport: ingest.Transport(transportText),
		Stored: stored, Codec: journal.Codec(codecText), Size: originalSize, Hash: hash,
		Metadata: ingest.JournalMetadata{
			Source: sourceID, NormalizerVersion: normalizerVersion, NormalizationStatus: status,
			Harness: harness.ReceiptEvidence{State: harness.ReceiptState(harnessState), Scope: harnessScope, Fingerprint: harnessFingerprint, Label: harnessLabel},
		},
		NormalizationError: normalizationError,
	}, nil
}

func (reader *legacyReader) Close() error {
	return errors.Join(reader.rows.Err(), reader.rows.Close())
}

func parseHash(text string) ([sha256.Size]byte, error) {
	decoded, err := hex.DecodeString(text)
	if err != nil || len(decoded) != sha256.Size {
		return [sha256.Size]byte{}, fmt.Errorf("invalid legacy payload hash %q", text)
	}
	var hash [sha256.Size]byte
	copy(hash[:], decoded)
	return hash, nil
}
