package planusage_test

import (
	"context"
	"testing"
	"time"

	"github.com/theoden9014/agentmetry/internal/planusage"
)

type parserStub struct{}

func (parserStub) ID() string { return "example" }
func (parserStub) Parse(raw []byte, capturedAt time.Time) ([]planusage.Snapshot, error) {
	return []planusage.Snapshot{{
		Source: "example", WindowID: "window", UsedPercent: 25,
		CapturedAt: capturedAt, Authority: "verified_parser", Raw: raw,
	}}, nil
}

type writerStub struct{ snapshots []planusage.Snapshot }

func (writer *writerStub) PutPlanUsage(_ context.Context, snapshot planusage.Snapshot) error {
	writer.snapshots = append(writer.snapshots, snapshot)
	return nil
}

func TestImporterAssignsSnapshotSemanticsThroughTheResolvedParser(t *testing.T) {
	writer := &writerStub{}
	importer := planusage.NewImporter(writer, func(source string) (planusage.Parser, bool) {
		return parserStub{}, source == "example"
	})
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)

	snapshots, err := importer.ImportRaw(context.Background(), "example", []byte(`{"used":25}`), now)

	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 1 || len(writer.snapshots) != 1 || writer.snapshots[0].Authority != "verified_parser" {
		t.Fatalf("unexpected imported snapshots: %#v", writer.snapshots)
	}
}
