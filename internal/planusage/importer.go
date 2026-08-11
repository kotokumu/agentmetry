package planusage

import (
	"context"
	"fmt"
	"time"
)

type ParserResolver func(string) (Parser, bool)

type RawImporter interface {
	ImportRaw(context.Context, string, []byte, time.Time) ([]Snapshot, error)
}

type Importer struct {
	writer  Writer
	resolve ParserResolver
}

func NewImporter(writer Writer, resolve ParserResolver) Importer {
	return Importer{writer: writer, resolve: resolve}
}

func (importer Importer) ImportRaw(ctx context.Context, source string, payload []byte, capturedAt time.Time) ([]Snapshot, error) {
	parser, ok := importer.resolve(source)
	if !ok {
		return nil, fmt.Errorf("unsupported plan usage source %q", source)
	}
	snapshots, err := parser.Parse(payload, capturedAt)
	if err != nil {
		return nil, err
	}
	for _, snapshot := range snapshots {
		if err := importer.writer.PutPlanUsage(ctx, snapshot); err != nil {
			return nil, err
		}
	}
	return snapshots, nil
}
