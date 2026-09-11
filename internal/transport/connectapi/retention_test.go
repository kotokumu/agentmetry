package connectapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/kotokumu/agentmetry/gen/agentmetry/v1"
	"github.com/kotokumu/agentmetry/gen/agentmetry/v1/agentmetryv1connect"
	"github.com/kotokumu/agentmetry/internal/archivefs"
	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/ingest"
	"github.com/kotokumu/agentmetry/internal/ingest/otel"
	"github.com/kotokumu/agentmetry/internal/retention"
	"github.com/kotokumu/agentmetry/internal/source/builtin"
	"github.com/kotokumu/agentmetry/internal/storage/sqlite"
	"github.com/kotokumu/agentmetry/sourceplugin"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type postPONRRepository struct {
	retention.Repository
	classifications atomic.Int64
}

type capacityWireRepository struct {
	retention.Repository
	report retention.CapacityReport
}

type mixedPostPONRRepository struct {
	retention.Repository
	failed atomic.Int64
}

func (*mixedPostPONRRepository) ClassifyRestoreScope(context.Context, retention.RestoreScope) (retention.RestoreClassification, error) {
	return retention.RestoreClassification{Result: "current", ArchivedMatches: 2}, nil
}

func (*mixedPostPONRRepository) ClaimRestore(_ context.Context, scope retention.RestoreScope, _ time.Time) (retention.RestoreClaim, error) {
	return retention.RestoreClaim{OperationID: "mixed-restore", Scope: scope, Classification: retention.RestoreClassification{Result: "current", ArchivedMatches: 1, DeletedMatches: 1}}, nil
}

func (*mixedPostPONRRepository) RetentionCapacity(context.Context, time.Time) (retention.CapacityReport, error) {
	return retention.CapacityReport{}, nil
}

func (repository *mixedPostPONRRepository) FailRestore(context.Context, string, error, time.Time) error {
	repository.failed.Add(1)
	return nil
}

func (repository *capacityWireRepository) RetentionCapacity(context.Context, time.Time) (retention.CapacityReport, error) {
	return repository.report, nil
}

func (repository *postPONRRepository) ClassifyRestoreScope(context.Context, retention.RestoreScope) (retention.RestoreClassification, error) {
	if repository.classifications.Add(1) == 1 {
		return retention.RestoreClassification{Result: string(retention.SegmentCurrent)}, nil
	}
	return retention.RestoreClassification{Result: string(retention.SegmentDeleted), DeletedMatches: 3}, nil
}

func (*postPONRRepository) ClaimRestore(context.Context, retention.RestoreScope, time.Time) (retention.RestoreClaim, error) {
	return retention.RestoreClaim{Classification: retention.RestoreClassification{Result: string(retention.SegmentDeleted), DeletedMatches: 3}}, nil
}

func TestRetentionConnectErrorMappings(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		code connect.Code
	}{
		{name: "invalid hold", err: retention.ErrInvalidHold, code: connect.CodeInvalidArgument},
		{name: "corrupt payload", err: retention.ErrArchivePayload, code: connect.CodeDataLoss},
		{name: "unverifiable metadata", err: retention.ErrArchiveMetadata, code: connect.CodeDataLoss},
		{name: "unsupported format", err: retention.ErrArchiveFormat, code: connect.CodeUnimplemented},
		{name: "capacity", err: retention.ErrInsufficientCapacity, code: connect.CodeResourceExhausted},
		{name: "conflict", err: retention.ErrRestoreConflict, code: connect.CodeAborted},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := connect.CodeOf(retentionConnectError(errors.New("wrapper: " + test.err.Error()))); got == test.code {
				t.Fatal("plain text wrapper unexpectedly preserved error identity")
			}
			if got := connect.CodeOf(retentionConnectError(test.err)); got != test.code {
				t.Fatalf("code=%v want=%v", got, test.code)
			}
		})
	}
}

func TestRestoreArchiveReclassifiesCommittedDeletionAsDeleted(t *testing.T) {
	repository := &postPONRRepository{}
	service := retention.NewServiceWithReplayer(repository, nil, nil, sourceplugin.NewRegistry(), time.Now)
	server := &RetentionServer{service: service}
	hold := int32(7)
	response, err := server.RestoreArchive(context.Background(), connect.NewRequest(&v1.RestoreArchiveRequest{Scope: &v1.RestoreArchiveRequest_SegmentId{SegmentId: "committed-delete"}, HoldDays: &hold}))
	if err != nil || response.Msg.Accepted || response.Msg.OperationId != "" || response.Msg.Result != "deleted" || response.Msg.DeletedMatches != 3 {
		t.Fatalf("response=%v err=%v", response, err)
	}
}

func TestRestoreArchiveUsesClaimClassificationForMixedPostPONRPeriod(t *testing.T) {
	repository := &mixedPostPONRRepository{}
	service := retention.NewServiceWithReplayer(repository, nil, nil, sourceplugin.NewRegistry(), time.Now)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := service.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	server := &RetentionServer{service: service}
	hold := int32(7)
	start := timestamppb.New(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	end := timestamppb.New(start.AsTime().Add(time.Hour))
	response, err := server.RestoreArchive(context.Background(), connect.NewRequest(&v1.RestoreArchiveRequest{Scope: &v1.RestoreArchiveRequest_ReceiveTime{ReceiveTime: &v1.ReceiveTimeRange{Start: start, End: end}}, HoldDays: &hold}))
	if err != nil || !response.Msg.Accepted || response.Msg.OperationId != "mixed-restore" || response.Msg.ArchivedMatches != 1 || response.Msg.DeletedMatches != 1 {
		t.Fatalf("response=%v err=%v", response, err)
	}
}

func TestRetentionCapacityMapsEveryWireField(t *testing.T) {
	free := int64(13)
	report := retention.CapacityReport{
		ObservedAt:                time.Date(2026, 9, 11, 12, 0, 0, 123, time.UTC),
		DatabaseBytes:             1,
		DatabaseUnusedBytes:       2,
		WALBytes:                  3,
		ArchiveBytes:              4,
		TotalAllocatedBytes:       5,
		ArchiveAllocatedBytes:     6,
		StagingAllocatedBytes:     7,
		ActiveRawBytes:            8,
		ObservationBytes:          9,
		QueryProjectionBytes:      10,
		EstimatedArchivePeakBytes: 11,
		EstimatedRestorePeakBytes: 12,
		FilesystemFreeBytes:       free,
		FilesystemAvailable:       true,
		UnavailableReason:         "capacity detail",
		Warning:                   "capacity warning",
	}
	service := retention.NewServiceWithReplayer(&capacityWireRepository{report: report}, nil, nil, sourceplugin.NewRegistry(), time.Now)
	response, err := (&RetentionServer{service: service}).GetRetentionCapacity(context.Background(), connect.NewRequest(&v1.GetRetentionCapacityRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	message := response.Msg
	if !message.ObservedAt.AsTime().Equal(report.ObservedAt) || message.DatabaseBytes != 1 || message.DatabaseUnusedBytes != 2 || message.WalBytes != 3 || message.ArchiveBytes != 4 || message.FilesystemFreeBytes == nil || *message.FilesystemFreeBytes != free || message.UnavailableReason != "capacity detail" || message.TotalAllocatedBytes != 5 || message.ArchiveAllocatedBytes != 6 || message.StagingAllocatedBytes != 7 || message.ActiveRawBytes != 8 || message.ObservationBytes != 9 || message.QueryProjectionBytes != 10 || message.EstimatedArchivePeakBytes != 11 || message.EstimatedRestorePeakBytes != 12 || message.Warning != "capacity warning" {
		t.Fatalf("capacity response=%v", message)
	}
}

func TestRetentionConnectPolicyValidationAndCapacity(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "retention-connect.db"), builtin.Registry())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	service := retention.NewServiceWithReplayer(store, archivefs.New(store.ArchiveDirectory()), retention.ReplayFunc(otel.ReplayExport), builtin.Registry(), func() time.Time { return now })
	path, handler := NewRetention(service)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	server := httptest.NewServer(mux)
	defer server.Close()
	client := agentmetryv1connect.NewAgentmetryRetentionServiceClient(http.DefaultClient, server.URL)
	status, err := client.GetRetentionStatus(context.Background(), connect.NewRequest(&v1.GetRetentionStatusRequest{}))
	if err != nil || status.Msg.GetPolicy().GetEnabled() {
		t.Fatalf("initial status=%v err=%v", status, err)
	}
	_, err = client.UpdateRetentionPolicy(context.Background(), connect.NewRequest(&v1.UpdateRetentionPolicyRequest{Enabled: true}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("missing cutoff code=%v err=%v", connect.CodeOf(err), err)
	}
	archiveDays, deleteDays := int32(30), int32(365)
	_, err = client.UpdateRetentionPolicy(context.Background(), connect.NewRequest(&v1.UpdateRetentionPolicyRequest{Enabled: false, ArchiveDays: &archiveDays}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("disabled cutoff code=%v err=%v", connect.CodeOf(err), err)
	}
	updated, err := client.UpdateRetentionPolicy(context.Background(), connect.NewRequest(&v1.UpdateRetentionPolicyRequest{Enabled: true, ArchiveDays: &archiveDays, DeleteDays: &deleteDays}))
	if err != nil || updated.Msg.GetPolicy().GetArchiveDays() != 30 || updated.Msg.GetPolicy().GetDeleteDays() != 365 {
		t.Fatalf("updated=%v err=%v", updated, err)
	}
	capacity, err := client.GetRetentionCapacity(context.Background(), connect.NewRequest(&v1.GetRetentionCapacityRequest{}))
	if err != nil || capacity.Msg.DatabaseBytes <= 0 || capacity.Msg.ObservedAt == nil {
		t.Fatalf("capacity=%v err=%v", capacity, err)
	}
	lookup, err := client.RestoreArchive(context.Background(), connect.NewRequest(&v1.RestoreArchiveRequest{Scope: &v1.RestoreArchiveRequest_SegmentId{SegmentId: "unknown"}}))
	if err != nil || lookup.Msg.Result != "not_found" || lookup.Msg.Accepted {
		t.Fatalf("lookup=%v err=%v", lookup, err)
	}
	hold := int32(7)
	_, err = client.RestoreArchive(context.Background(), connect.NewRequest(&v1.RestoreArchiveRequest{Scope: &v1.RestoreArchiveRequest_SegmentId{SegmentId: "unknown"}, HoldDays: &hold}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("terminal lookup hold code=%v err=%v", connect.CodeOf(err), err)
	}
	accepted := ingest.AcceptedExport{
		Envelope:           ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, now.Add(-31*retention.Day), []byte{0x0a, 0x00}),
		Journal:            ingest.JournalMetadata{Source: "unknown", NormalizerVersion: 1, NormalizationStatus: "failed"},
		NormalizationError: "fixture",
	}
	if err := store.CommitExport(context.Background(), accepted); err != nil {
		t.Fatal(err)
	}
	secondReceivedAt := now.Add(-32 * retention.Day)
	accepted.Envelope = ingest.NewEnvelope(canonical.SignalLog, ingest.TransportGRPC, secondReceivedAt, []byte{0x0a, 0x01})
	if err := store.CommitExport(context.Background(), accepted); err != nil {
		t.Fatal(err)
	}
	if err := service.RunMaintenance(context.Background()); err != nil {
		t.Fatal(err)
	}
	segments, _, err := service.Segments(context.Background(), "", 10)
	if err != nil || len(segments) != 2 {
		t.Fatalf("segments=%v err=%v", segments, err)
	}
	directSegmentID, periodSegmentID := segments[0].ID, segments[1].ID
	if segments[0].MinReceivedAt.Equal(secondReceivedAt) {
		directSegmentID, periodSegmentID = segments[1].ID, segments[0].ID
	}
	firstPage, err := client.ListArchiveSegments(context.Background(), connect.NewRequest(&v1.ListArchiveSegmentsRequest{PageSize: 1}))
	if err != nil || len(firstPage.Msg.Segments) != 1 || firstPage.Msg.NextPageToken == "" || firstPage.Msg.Segments[0].PayloadIntegrity != "intact" || firstPage.Msg.Segments[0].MetadataIntegrity != "verifiable" || firstPage.Msg.Segments[0].ScheduledDeleteAt == nil {
		t.Fatalf("first inventory page=%v err=%v", firstPage, err)
	}
	secondPage, err := client.ListArchiveSegments(context.Background(), connect.NewRequest(&v1.ListArchiveSegmentsRequest{PageSize: 1, PageToken: firstPage.Msg.NextPageToken}))
	if err != nil || len(secondPage.Msg.Segments) != 1 || secondPage.Msg.NextPageToken != "" {
		t.Fatalf("second inventory page=%v err=%v", secondPage, err)
	}
	wantSegments := make(map[string]retention.Segment, len(segments))
	for _, segment := range segments {
		wantSegments[segment.ID] = segment
	}
	for _, actual := range append(firstPage.Msg.Segments, secondPage.Msg.Segments...) {
		want, ok := wantSegments[actual.Id]
		if !ok || actual.MinReceivedAt == nil || actual.MaxReceivedAt == nil || !actual.MinReceivedAt.AsTime().Equal(want.MinReceivedAt) || !actual.MaxReceivedAt.AsTime().Equal(want.MaxReceivedAt) || actual.ExportCount != int64(want.ExportCount) || actual.OriginalBytes != want.OriginalBytes || actual.StoredBytes != want.StoredBytes || actual.PayloadIntegrity != string(want.PayloadIntegrity) || actual.MetadataIntegrity != string(want.MetadataIntegrity) || actual.ScheduledDeleteAt == nil || want.ScheduledDeleteAt == nil || !actual.ScheduledDeleteAt.AsTime().Equal(*want.ScheduledDeleteAt) || actual.ScheduleUnavailableReason != want.ScheduleUnavailable || actual.IntegrityError != want.IntegrityError {
			t.Fatalf("inventory segment=%v want=%+v", actual, want)
		}
	}
	_, err = client.RestoreArchive(context.Background(), connect.NewRequest(&v1.RestoreArchiveRequest{Scope: &v1.RestoreArchiveRequest_SegmentId{SegmentId: directSegmentID}}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("current segment without hold code=%v err=%v", connect.CodeOf(err), err)
	}
	invalidHold := int32(0)
	_, err = client.RestoreArchive(context.Background(), connect.NewRequest(&v1.RestoreArchiveRequest{Scope: &v1.RestoreArchiveRequest_SegmentId{SegmentId: directSegmentID}, HoldDays: &invalidHold}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("invalid hold code=%v err=%v", connect.CodeOf(err), err)
	}
	restored, err := client.RestoreArchive(context.Background(), connect.NewRequest(&v1.RestoreArchiveRequest{Scope: &v1.RestoreArchiveRequest_SegmentId{SegmentId: directSegmentID}, HoldDays: &hold}))
	if err != nil || !restored.Msg.Accepted || restored.Msg.OperationId == "" || restored.Msg.Result != "current" {
		t.Fatalf("segment restore=%v err=%v", restored, err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		status, err = client.GetRetentionStatus(context.Background(), connect.NewRequest(&v1.GetRetentionStatusRequest{}))
		if err != nil {
			t.Fatal(err)
		}
		terminal := false
		for _, operation := range status.Msg.Operations {
			if operation.Id == restored.Msg.OperationId && operation.Status == "completed" && operation.CompletedAt != nil && operation.AffectedExports == 1 && operation.AffectedSegments == 1 {
				terminal = true
			}
		}
		if terminal {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("restore operation did not complete: %v", status.Msg.Operations)
		}
		time.Sleep(10 * time.Millisecond)
	}
	restoredLookup, err := client.RestoreArchive(context.Background(), connect.NewRequest(&v1.RestoreArchiveRequest{Scope: &v1.RestoreArchiveRequest_SegmentId{SegmentId: directSegmentID}}))
	if err != nil || restoredLookup.Msg.Result != "restored" || restoredLookup.Msg.Accepted || len(restoredLookup.Msg.CurrentSegmentIds) != 0 {
		t.Fatalf("restored lookup=%v err=%v", restoredLookup, err)
	}
	periodRestored, err := client.RestoreArchive(context.Background(), connect.NewRequest(&v1.RestoreArchiveRequest{
		Scope:    &v1.RestoreArchiveRequest_ReceiveTime{ReceiveTime: &v1.ReceiveTimeRange{Start: timestamppb.New(secondReceivedAt.Add(-time.Minute)), End: timestamppb.New(secondReceivedAt.Add(time.Minute))}},
		HoldDays: &hold,
	}))
	if err != nil || !periodRestored.Msg.Accepted || periodRestored.Msg.OperationId == "" || periodRestored.Msg.ArchivedMatches != 1 {
		t.Fatalf("period restore=%v err=%v", periodRestored, err)
	}
	if periodSegmentID == "" {
		t.Fatal("period segment identity was lost")
	}
}
