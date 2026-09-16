package retention

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kotokumu/agentmetry/sourceplugin"
)

func TestArchiveGroupBoundaryUsesUTCDateAndOriginalByteBound(t *testing.T) {
	at := time.Date(2026, 9, 10, 23, 59, 0, 0, time.FixedZone("west", -7*60*60))
	for _, test := range []struct {
		name    string
		firstAt time.Time
		first   int64
		nextAt  time.Time
		next    int64
		limit   int64
		want    bool
	}{
		{name: "exact bound remains in the group", firstAt: at, first: 4, nextAt: at.Add(time.Minute), next: 6, limit: 10, want: true},
		{name: "overflow starts the next group", firstAt: at, first: 4, nextAt: at.Add(time.Minute), next: 7, limit: 10},
		{name: "UTC date starts the next group", firstAt: at, first: 4, nextAt: at.Add(18 * time.Hour), next: 1, limit: 10},
		{name: "oversized first export is admitted alone", firstAt: at, first: 11, nextAt: at.Add(time.Minute), next: 1, limit: 10},
	} {
		t.Run(test.name, func(t *testing.T) {
			var boundary ArchiveGroupBoundary
			if !boundary.TryInclude(test.firstAt, test.first, test.limit) {
				t.Fatal("empty group rejected its first export")
			}
			if got := boundary.TryInclude(test.nextAt, test.next, test.limit); got != test.want {
				t.Fatalf("TryInclude()=%v, want %v", got, test.want)
			}
		})
	}
}

type boundedMaintenanceRepository struct {
	Repository
	groups           [][]RawExport
	next             int
	planned          int
	planningComplete bool
	groupOutstanding bool
	selectionCalls   int
	completionErr    error
}

func (*boundedMaintenanceRepository) BeginRetentionCycle(context.Context, time.Time) (string, error) {
	return "bounded-cycle", nil
}

func (*boundedMaintenanceRepository) RetentionPolicy(context.Context) (Policy, time.Time, error) {
	policy, _ := NewPolicy(1, 30, 1)
	return policy, time.Time{}, nil
}

func (*boundedMaintenanceRepository) ResumeRetentionDeletions(context.Context) error { return nil }

func (*boundedMaintenanceRepository) EligibleDeletionSegmentsForCycle(context.Context, Policy, time.Time, string) ([]Segment, error) {
	return nil, nil
}

func (repository *boundedMaintenanceRepository) CountArchiveGroupsForCycle(context.Context, Policy, time.Time, string, int64) (int, error) {
	return len(repository.groups), nil
}

func (repository *boundedMaintenanceRepository) MarkRetentionCyclePlanned(_ context.Context, _ string, planned int) error {
	repository.planned = planned
	repository.planningComplete = true
	return nil
}

func (repository *boundedMaintenanceRepository) NextArchiveGroupForCycle(context.Context, Policy, time.Time, string, int64) ([]RawExport, error) {
	repository.selectionCalls++
	if !repository.planningComplete {
		return nil, errors.New("selected before planning completed")
	}
	if repository.groupOutstanding {
		return nil, errors.New("selected while prior group remained unresolved")
	}
	if repository.next >= len(repository.groups) {
		return nil, nil
	}
	repository.groupOutstanding = true
	return repository.groups[repository.next], nil
}

func (repository *boundedMaintenanceRepository) ClaimArchive(_ context.Context, exports []RawExport, _ Policy, _ time.Time, _ string) (ArchiveClaim, error) {
	if !repository.groupOutstanding || len(exports) == 0 {
		return ArchiveClaim{}, errors.New("claimed without one selected group")
	}
	return ArchiveClaim{OperationID: fmt.Sprintf("archive-%d", repository.next+1), Exports: exports}, nil
}

func (*boundedMaintenanceRepository) RetentionCapacity(context.Context, time.Time) (CapacityReport, error) {
	return CapacityReport{}, nil
}

func (repository *boundedMaintenanceRepository) PublishArchive(context.Context, ArchivePublication) error {
	if !repository.groupOutstanding {
		return errors.New("published without selected group")
	}
	repository.groupOutstanding = false
	repository.next++
	return nil
}

func (repository *boundedMaintenanceRepository) CompleteRetentionCycle(_ context.Context, _ string, cycleErr error) error {
	repository.completionErr = cycleErr
	return nil
}

type boundedArchiveStore struct{ SegmentStore }

func (*boundedArchiveStore) BuildWithIncarnation(_ context.Context, exports []RawExport, incarnation string) (ArchiveInstall, error) {
	return ArchiveInstall{SegmentID: incarnation, Path: incarnation + ".tar.zst", OriginalBytes: int64(len(exports)), StoredBytes: 1, ExportCount: len(exports)}, nil
}

func TestMaintenancePlansBeforeProcessingOneArchiveGroupAtATime(t *testing.T) {
	repository := &boundedMaintenanceRepository{groups: [][]RawExport{
		{{ID: 1, ReceivedAt: time.Now(), Protobuf: []byte{1}}},
		{{ID: 2, ReceivedAt: time.Now(), Protobuf: []byte{2}}},
	}}
	service := NewServiceWithReplayer(repository, &boundedArchiveStore{}, nil, sourceplugin.NewRegistry(), time.Now)

	if err := service.RunMaintenance(context.Background()); err != nil {
		t.Fatal(err)
	}
	if repository.planned != 2 || repository.next != 2 || repository.selectionCalls != 2 || repository.completionErr != nil {
		t.Fatalf("planned=%d published=%d selections=%d completion=%v", repository.planned, repository.next, repository.selectionCalls, repository.completionErr)
	}
}

func TestMaintenanceFailsWhenPlannedArchiveGroupIsMissing(t *testing.T) {
	repository := &boundedMaintenanceRepository{groups: [][]RawExport{nil}}
	service := NewServiceWithReplayer(repository, &boundedArchiveStore{}, nil, sourceplugin.NewRegistry(), time.Now)

	err := service.RunMaintenance(context.Background())
	if err == nil || !strings.Contains(err.Error(), "planned archive group 1 is unavailable") {
		t.Fatalf("RunMaintenance() error=%v", err)
	}
	if repository.next != 0 || repository.completionErr == nil {
		t.Fatalf("published=%d completion=%v", repository.next, repository.completionErr)
	}
}

func TestMaintenanceCompletesAnEmptyPlanWithoutSelecting(t *testing.T) {
	repository := &boundedMaintenanceRepository{}
	service := NewServiceWithReplayer(repository, &boundedArchiveStore{}, nil, sourceplugin.NewRegistry(), time.Now)

	if err := service.RunMaintenance(context.Background()); err != nil {
		t.Fatal(err)
	}
	if repository.planned != 0 || repository.selectionCalls != 0 || repository.completionErr != nil {
		t.Fatalf("planned=%d selections=%d completion=%v", repository.planned, repository.selectionCalls, repository.completionErr)
	}
}

type cadenceRepository struct {
	Repository
	starts chan string
	next   atomic.Int64
}

type setupFailureRepository struct {
	Repository
	completed chan error
}

type failureThenSuccessRepository struct {
	Repository
	next        atomic.Int64
	policyCalls atomic.Int64
	completed   chan maintenanceResult
}

type maintenanceResult struct {
	id  string
	err error
}

type retryRepository struct {
	Repository
	retries          atomic.Int64
	completeAttempts atomic.Int64
}

func (repository *retryRepository) RecordRestoreRetry(context.Context, string, error, time.Time) error {
	repository.retries.Add(1)
	return nil
}

func (repository *retryRepository) CompleteRestore(context.Context, string, time.Time) error {
	if repository.completeAttempts.Add(1) == 1 {
		return errors.New("injected terminal publication failure")
	}
	return nil
}

type retryArchiveStore struct {
	SegmentStore
	removeAttempts atomic.Int64
}

type insufficientCapacityRepository struct {
	Repository
	failed atomic.Int64
}

type restoreFailureRepository struct {
	Repository
	failed      atomic.Int64
	verified    atomic.Int64
	published   atomic.Int64
	lastFailure error
}

type failingArchiveStore struct {
	SegmentStore
	err error
}

func (*insufficientCapacityRepository) RetentionCapacity(context.Context, time.Time) (CapacityReport, error) {
	return CapacityReport{FilesystemAvailable: true, FilesystemFreeBytes: 1}, nil
}

func (repository *insufficientCapacityRepository) FailRestore(context.Context, string, error, time.Time) error {
	repository.failed.Add(1)
	return nil
}

func (*restoreFailureRepository) RetentionCapacity(context.Context, time.Time) (CapacityReport, error) {
	return CapacityReport{}, nil
}

func (repository *restoreFailureRepository) FailRestore(_ context.Context, _ string, failure error, _ time.Time) error {
	repository.failed.Add(1)
	repository.lastFailure = failure
	return nil
}

func (repository *restoreFailureRepository) RecordSegmentVerificationFailure(context.Context, string, PayloadIntegrity, MetadataIntegrity, time.Time, error) error {
	repository.verified.Add(1)
	return nil
}

func (repository *restoreFailureRepository) PublishRestore(context.Context, RestorePublication) error {
	repository.published.Add(1)
	return nil
}

func (store *failingArchiveStore) OpenVerified(context.Context, string) (VerifiedArchive, error) {
	return VerifiedArchive{}, store.err
}

func (store *retryArchiveStore) Remove(string) error {
	if store.removeAttempts.Add(1) == 1 {
		return errors.New("injected archive cleanup failure")
	}
	return nil
}

func (*setupFailureRepository) BeginRetentionCycle(context.Context, time.Time) (string, error) {
	return "cycle-with-setup-failure", nil
}

func (*setupFailureRepository) RetentionPolicy(context.Context) (Policy, time.Time, error) {
	return Policy{}, time.Time{}, errors.New("injected policy read failure")
}

func (repository *setupFailureRepository) CompleteRetentionCycle(_ context.Context, _ string, cycleErr error) error {
	repository.completed <- cycleErr
	return nil
}

func (*setupFailureRepository) ResumeRetentionDeletions(context.Context) error { return nil }

func (repository *failureThenSuccessRepository) BeginRetentionCycle(context.Context, time.Time) (string, error) {
	return fmt.Sprintf("cycle-%d", repository.next.Add(1)), nil
}

func (repository *failureThenSuccessRepository) RetentionPolicy(context.Context) (Policy, time.Time, error) {
	if repository.policyCalls.Add(1) == 1 {
		return Policy{}, time.Time{}, errors.New("injected first-cycle failure")
	}
	policy, _ := NewPolicy(1, 30, 1)
	return policy, time.Time{}, nil
}

func (repository *failureThenSuccessRepository) CompleteRetentionCycle(_ context.Context, id string, cycleErr error) error {
	repository.completed <- maintenanceResult{id: id, err: cycleErr}
	return nil
}

func (*failureThenSuccessRepository) ResumeRetentionDeletions(context.Context) error { return nil }
func (*failureThenSuccessRepository) MarkRetentionCyclePlanned(context.Context, string, int) error {
	return nil
}
func (*failureThenSuccessRepository) EligibleDeletionSegmentsForCycle(context.Context, Policy, time.Time, string) ([]Segment, error) {
	return nil, nil
}
func (*failureThenSuccessRepository) CountArchiveGroupsForCycle(context.Context, Policy, time.Time, string, int64) (int, error) {
	return 0, nil
}
func (*failureThenSuccessRepository) NextArchiveGroupForCycle(context.Context, Policy, time.Time, string, int64) ([]RawExport, error) {
	return nil, nil
}

func (repository *cadenceRepository) RetentionPolicy(context.Context) (Policy, time.Time, error) {
	policy, _ := NewPolicy(1, 30, 1)
	return policy, time.Time{}, nil
}

func (repository *cadenceRepository) BeginRetentionCycle(_ context.Context, _ time.Time) (string, error) {
	id := fmt.Sprintf("cycle-%d", repository.next.Add(1))
	repository.starts <- id
	return id, nil
}

func (*cadenceRepository) CompleteRetentionCycle(context.Context, string, error) error { return nil }

func (*cadenceRepository) MarkRetentionCyclePlanned(context.Context, string, int) error { return nil }

func (*cadenceRepository) ResumeRetentionDeletions(context.Context) error { return nil }

func (*cadenceRepository) EligibleDeletionSegmentsForCycle(ctx context.Context, _ Policy, _ time.Time, _ string) ([]Segment, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (*cadenceRepository) CountArchiveGroupsForCycle(context.Context, Policy, time.Time, string, int64) (int, error) {
	return 0, nil
}
func (*cadenceRepository) NextArchiveGroupForCycle(context.Context, Policy, time.Time, string, int64) ([]RawExport, error) {
	return nil, nil
}

func TestSchedulerStartsNextCycleFromTickerCadenceWhilePriorWorkIsBlocked(t *testing.T) {
	repository := &cadenceRepository{starts: make(chan string, 8)}
	service := NewServiceWithReplayer(repository, nil, nil, sourceplugin.NewRegistry(), time.Now)
	scheduler := startScheduler(context.Background(), service, 20*time.Millisecond)
	for index := 0; index < 2; index++ {
		select {
		case <-repository.starts:
		case <-time.After(300 * time.Millisecond):
			t.Fatal("scheduler did not durably start the next cadence cycle")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := scheduler.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestMaintenanceRecordsSetupFailureAfterDurableCycleStart(t *testing.T) {
	repository := &setupFailureRepository{completed: make(chan error, 1)}
	service := NewServiceWithReplayer(repository, nil, nil, sourceplugin.NewRegistry(), time.Now)
	err := service.RunMaintenance(context.Background())
	if err == nil || err.Error() != "injected policy read failure" {
		t.Fatalf("maintenance error=%v", err)
	}
	select {
	case completedErr := <-repository.completed:
		if completedErr == nil || completedErr.Error() != err.Error() {
			t.Fatalf("completed cycle error=%v, want %v", completedErr, err)
		}
	default:
		t.Fatal("setup failure did not complete the durable cycle")
	}
}

func TestSchedulerRunsNextCadenceAfterPriorCycleFails(t *testing.T) {
	repository := &failureThenSuccessRepository{completed: make(chan maintenanceResult, 4)}
	service := NewServiceWithReplayer(repository, nil, nil, sourceplugin.NewRegistry(), time.Now)
	scheduler := startScheduler(context.Background(), service, 20*time.Millisecond)
	results := make([]maintenanceResult, 0, 2)
	deadline := time.After(500 * time.Millisecond)
	for len(results) < 2 {
		select {
		case result := <-repository.completed:
			results = append(results, result)
		case <-deadline:
			t.Fatal("scheduler did not run the cadence following a failed cycle")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := scheduler.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if results[0].err == nil || results[0].err.Error() != "injected first-cycle failure" || results[1].err != nil || results[0].id == results[1].id {
		t.Fatalf("cycle results=%+v", results)
	}
}

func TestPublishedRestoreCleanupRetriesUntilOperationCanBecomeTerminal(t *testing.T) {
	repository := &retryRepository{}
	archives := &retryArchiveStore{}
	service := NewServiceWithReplayer(repository, archives, nil, sourceplugin.NewRegistry(), time.Now)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := service.removePublishedArchive(ctx, "restore", "segment"); err != nil {
		t.Fatal(err)
	}
	if err := service.completePublishedRestore(ctx, "restore"); err != nil {
		t.Fatal(err)
	}
	if archives.removeAttempts.Load() != 2 || repository.completeAttempts.Load() != 2 || repository.retries.Load() != 2 {
		t.Fatalf("remove=%d complete=%d retries=%d", archives.removeAttempts.Load(), repository.completeAttempts.Load(), repository.retries.Load())
	}
}

func TestRestoreVerificationFailuresRemainAtomicAndTerminal(t *testing.T) {
	for _, test := range []struct {
		name             string
		failure          error
		wantVerification int64
	}{
		{name: "unsupported format", failure: ErrArchiveFormat},
		{name: "corrupt payload", failure: ErrArchivePayload, wantVerification: 1},
		{name: "storage read", failure: errors.New("injected archive read failure"), wantVerification: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &restoreFailureRepository{}
			service := NewServiceWithReplayer(repository, &failingArchiveStore{err: test.failure}, nil, sourceplugin.NewRegistry(), time.Now)
			claim := RestoreClaim{OperationID: "restore-failure", Scope: mustSegmentScopeForServiceTest(t, "segment"), Segments: []Segment{{ID: "segment", OriginalBytes: 1024, PayloadIntegrity: PayloadIntact, MetadataIntegrity: MetadataVerifiable}}}
			err := service.restoreClaim(context.Background(), claim, Hold{Days: 1})
			if err == nil || repository.failed.Load() != 1 || repository.verified.Load() != test.wantVerification || repository.published.Load() != 0 || repository.lastFailure == nil {
				t.Fatalf("err=%v failed=%d verified=%d published=%d terminal=%v", err, repository.failed.Load(), repository.verified.Load(), repository.published.Load(), repository.lastFailure)
			}
		})
	}
}

func mustSegmentScopeForServiceTest(t *testing.T, id string) RestoreScope {
	t.Helper()
	scope, err := SegmentScope(id)
	if err != nil {
		t.Fatal(err)
	}
	return scope
}

func TestRestoreRejectsInsufficientCapacityBeforeReadingOrPublishingArchive(t *testing.T) {
	repository := &insufficientCapacityRepository{}
	service := NewServiceWithReplayer(repository, nil, nil, sourceplugin.NewRegistry(), time.Now)
	err := service.restoreClaim(context.Background(), RestoreClaim{OperationID: "restore", Segments: []Segment{{ID: "segment", OriginalBytes: 2}}, Scope: RestoreScope{Kind: RestoreBySegment, SegmentID: "segment"}}, mustTestHold(t, 1))
	if !errors.Is(err, ErrInsufficientCapacity) || repository.failed.Load() != 1 {
		t.Fatalf("error=%v failed=%d", err, repository.failed.Load())
	}
}

func mustTestHold(t *testing.T, days int) Hold {
	t.Helper()
	hold, err := NewHold(days)
	if err != nil {
		t.Fatal(err)
	}
	return hold
}
