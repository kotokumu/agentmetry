package retention

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kotokumu/agentmetry/sourceplugin"
)

func TestPlanSegmentsGroupsByUTCDateAndOriginalByteBound(t *testing.T) {
	at := time.Date(2026, 9, 10, 23, 59, 0, 0, time.UTC)
	exports := []RawExport{
		{ID: 1, ReceivedAt: at, Protobuf: make([]byte, 4)},
		{ID: 2, ReceivedAt: at.Add(time.Minute), Protobuf: make([]byte, 4)},
		{ID: 3, ReceivedAt: at.Add(2 * time.Minute), Protobuf: make([]byte, 5)},
		{ID: 4, ReceivedAt: at.Add(3 * time.Minute), Protobuf: make([]byte, 5)},
	}
	groups := PlanSegments(exports, 10)
	if len(groups) != 3 || len(groups[0]) != 1 || len(groups[1]) != 2 || len(groups[2]) != 1 {
		t.Fatalf("unexpected segment plan: %#v", groups)
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
func (*failureThenSuccessRepository) EligibleActiveExportsForCycle(context.Context, Policy, time.Time, string) ([]RawExport, error) {
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

func (*cadenceRepository) EligibleActiveExportsForCycle(context.Context, Policy, time.Time, string) ([]RawExport, error) {
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
