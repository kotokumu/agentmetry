package retention

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/kotokumu/agentmetry/internal/canonical"
	"github.com/kotokumu/agentmetry/internal/harness"
	"github.com/kotokumu/agentmetry/internal/ingest"
	source "github.com/kotokumu/agentmetry/sourceplugin"
)

const MaxSegmentOriginalBytes int64 = 256 << 20

type PolicyRepository interface {
	RetentionPolicy(context.Context) (Policy, time.Time, error)
	UpdateRetentionPolicy(context.Context, bool, int, int, time.Time) (Policy, error)
}

type ArchiveRepository interface {
	EligibleActiveExportsForCycle(context.Context, Policy, time.Time, string) ([]RawExport, error)
	ClaimArchive(context.Context, []RawExport, Policy, time.Time, string) (ArchiveClaim, error)
	FailArchive(context.Context, string, error, time.Time) error
	PublishArchive(context.Context, ArchivePublication) error
	ListArchiveSegments(context.Context, string, int) ([]Segment, string, error)
}

type RestoreRepository interface {
	ClassifyRestoreScope(context.Context, RestoreScope) (RestoreClassification, error)
	ClaimRestore(context.Context, RestoreScope, time.Time) (RestoreClaim, error)
	FailRestore(context.Context, string, error, time.Time) error
	RecordRestoreRetry(context.Context, string, error, time.Time) error
	CompleteRestore(context.Context, string, time.Time) error
	RecordSegmentVerificationFailure(context.Context, string, PayloadIntegrity, MetadataIntegrity, time.Time, error) error
	PublishRestore(context.Context, RestorePublication) error
}

type ExpiryRepository interface {
	EligibleDeletionSegmentsForCycle(context.Context, Policy, time.Time, string) ([]Segment, error)
	DeleteArchiveSegment(context.Context, string, Policy, time.Time, string, DeletionHandle) error
	ResumeRetentionDeletions(context.Context) error
}

type StatusRepository interface {
	RetentionOperations(context.Context) ([]Operation, error)
	RetentionCapacity(context.Context, time.Time) (CapacityReport, error)
	BeginRetentionCycle(context.Context, time.Time) (string, error)
	MarkRetentionCyclePlanned(context.Context, string, int) error
	CompleteRetentionCycle(context.Context, string, error) error
	RetentionCycles(context.Context) ([]Cycle, error)
}

type Repository interface {
	PolicyRepository
	ArchiveRepository
	RestoreRepository
	ExpiryRepository
	StatusRepository
}

type SegmentStore interface {
	BuildWithIncarnation(context.Context, []RawExport, string) (ArchiveInstall, error)
	OpenVerified(context.Context, string) (VerifiedArchive, error)
	DeletionFile(string) (DeletionHandle, error)
	Remove(string) error
}

type ExportReplayer interface {
	Replay(canonical.Signal, ingest.Transport, time.Time, []byte, source.Registry) (ingest.AcceptedExport, error)
}

type ReplayFunc func(canonical.Signal, ingest.Transport, time.Time, []byte, source.Registry) (ingest.AcceptedExport, error)

func (replay ReplayFunc) Replay(signal canonical.Signal, transport ingest.Transport, receivedAt time.Time, protobuf []byte, profiles source.Registry) (ingest.AcceptedExport, error) {
	return replay(signal, transport, receivedAt, protobuf, profiles)
}

type Service struct {
	repository Repository
	archives   SegmentStore
	replayer   ExportReplayer
	profiles   source.Registry
	now        func() time.Time
	mu         sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
}

func NewServiceWithReplayer(repository Repository, archives SegmentStore, replayer ExportReplayer, profiles source.Registry, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{repository: repository, archives: archives, replayer: replayer, profiles: profiles, now: now, ctx: ctx, cancel: cancel}
}

func (service *Service) Policy(ctx context.Context) (Policy, time.Time, error) {
	return service.repository.RetentionPolicy(ctx)
}

func (service *Service) UpdatePolicy(ctx context.Context, enabled bool, archiveDays, deleteDays int) (Policy, error) {
	return service.repository.UpdateRetentionPolicy(ctx, enabled, archiveDays, deleteDays, service.now().UTC())
}

func (service *Service) Segments(ctx context.Context, after string, limit int) ([]Segment, string, error) {
	policy, _, err := service.repository.RetentionPolicy(ctx)
	if err != nil {
		return nil, "", err
	}
	segments, next, err := service.repository.ListArchiveSegments(ctx, after, limit)
	if err != nil {
		return nil, "", err
	}
	for index := range segments {
		if segments[index].MetadataIntegrity != MetadataVerifiable {
			segments[index].ScheduleUnavailable = "lifecycle metadata is unverifiable"
		} else if policy.Enabled {
			value := segments[index].MaxReceivedAt.Add(policy.DeleteAfter.Duration())
			segments[index].ScheduledDeleteAt = &value
		} else {
			segments[index].ScheduleUnavailable = "retention policy is disabled"
		}
	}
	return segments, next, nil
}

func (service *Service) Operations(ctx context.Context) ([]Operation, error) {
	return service.repository.RetentionOperations(ctx)
}

func (service *Service) ClassifyRestore(ctx context.Context, scope RestoreScope) (RestoreClassification, error) {
	return service.repository.ClassifyRestoreScope(ctx, scope)
}

func (service *Service) Cycles(ctx context.Context) ([]Cycle, error) {
	return service.repository.RetentionCycles(ctx)
}

func (service *Service) Capacity(ctx context.Context) (CapacityReport, error) {
	return service.repository.RetentionCapacity(ctx, service.now().UTC())
}

// RunMaintenance evaluates one fixed cohort. Exports admitted during the run
// are deliberately left for the next cycle.
func (service *Service) RunMaintenance(ctx context.Context) error {
	evaluatedAt := service.now().UTC()
	cycleID, err := service.repository.BeginRetentionCycle(ctx, evaluatedAt)
	if err != nil {
		if errors.Is(err, ErrRetentionDisabled) {
			return nil
		}
		return err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	runErr := service.repository.ResumeRetentionDeletions(ctx)
	if runErr == nil {
		runErr = service.runMaintenance(ctx, evaluatedAt, cycleID)
	}
	if completeErr := service.repository.CompleteRetentionCycle(ctx, cycleID, runErr); completeErr != nil {
		if runErr != nil {
			return fmt.Errorf("maintenance failed: %v; complete cycle: %w", runErr, completeErr)
		}
		return completeErr
	}
	return runErr
}

func (service *Service) runMaintenance(ctx context.Context, evaluatedAt time.Time, cycleID string) error {
	policy, _, err := service.repository.RetentionPolicy(ctx)
	if err != nil {
		return err
	}
	if !policy.Enabled {
		return service.repository.MarkRetentionCyclePlanned(ctx, cycleID, 0)
	}
	deletionSegments, err := service.repository.EligibleDeletionSegmentsForCycle(ctx, policy, evaluatedAt, cycleID)
	if err != nil {
		return err
	}
	exports, err := service.repository.EligibleActiveExportsForCycle(ctx, policy, evaluatedAt, cycleID)
	if err != nil {
		return err
	}
	archiveGroups := PlanSegments(exports, MaxSegmentOriginalBytes)
	if err := service.repository.MarkRetentionCyclePlanned(ctx, cycleID, len(archiveGroups)+len(deletionSegments)); err != nil {
		return err
	}
	for _, group := range archiveGroups {
		claim, err := service.repository.ClaimArchive(ctx, group, policy, evaluatedAt, cycleID)
		if err != nil {
			return err
		}
		group = claim.Exports
		capacity, err := service.repository.RetentionCapacity(ctx, evaluatedAt)
		if err != nil {
			_ = service.repository.FailArchive(context.Background(), claim.OperationID, err, service.now().UTC())
			return err
		}
		var candidateBytes int64
		for _, exported := range group {
			candidateBytes += int64(len(exported.Protobuf))
		}
		archivePeakBytes := candidateBytes * 2
		if capacity.FilesystemAvailable && capacity.FilesystemFreeBytes < archivePeakBytes {
			err := fmt.Errorf("%w: archive candidate needs up to %d bytes, have %d", ErrInsufficientCapacity, archivePeakBytes, capacity.FilesystemFreeBytes)
			_ = service.repository.FailArchive(context.Background(), claim.OperationID, err, service.now().UTC())
			return err
		}
		installed, err := service.archives.BuildWithIncarnation(ctx, group, cycleID)
		if err != nil {
			_ = service.repository.FailArchive(context.Background(), claim.OperationID, err, service.now().UTC())
			return err
		}
		publication := ArchivePublication{OperationID: claim.OperationID, CycleID: cycleID, SegmentID: installed.SegmentID, FileName: installed.Path,
			FileSHA256: installed.FileSHA256, MembershipSHA256: installed.MembershipSHA256,
			StoredBytes: installed.StoredBytes, OriginalBytes: installed.OriginalBytes, Exports: group,
			EvaluatedAt: evaluatedAt, PolicyRevision: policy.Revision}
		if err := service.repository.PublishArchive(ctx, publication); err != nil {
			_ = service.archives.Remove(installed.SegmentID)
			_ = service.repository.FailArchive(context.Background(), claim.OperationID, err, service.now().UTC())
			return err
		}
	}
	for _, segment := range deletionSegments {
		segmentID := segment.ID
		file, err := service.archives.DeletionFile(segmentID)
		if err != nil {
			return err
		}
		if err := service.repository.DeleteArchiveSegment(ctx, segmentID, policy, evaluatedAt, cycleID, file); err != nil {
			return err
		}
	}
	return nil
}

func PlanSegments(exports []RawExport, maxOriginalBytes int64) [][]RawExport {
	if maxOriginalBytes <= 0 {
		maxOriginalBytes = MaxSegmentOriginalBytes
	}
	var result [][]RawExport
	var current []RawExport
	var currentDate string
	var currentBytes int64
	for _, exported := range exports {
		date := exported.ReceivedAt.UTC().Format("2006-01-02")
		size := int64(len(exported.Protobuf))
		if len(current) > 0 && (date != currentDate || currentBytes+size > maxOriginalBytes) {
			result = append(result, current)
			current, currentBytes = nil, 0
		}
		if len(current) == 0 {
			currentDate = date
		}
		current = append(current, exported)
		currentBytes += size
	}
	if len(current) > 0 {
		result = append(result, current)
	}
	return result
}

func (service *Service) Restore(ctx context.Context, scope RestoreScope, hold Hold) error {
	service.mu.Lock()
	defer service.mu.Unlock()
	requestedAt := service.now().UTC()
	claim, err := service.repository.ClaimRestore(ctx, scope, requestedAt)
	if err != nil {
		return err
	}
	segments := claim.Segments
	if len(segments) == 0 {
		return nil
	}
	return service.restoreClaim(ctx, claim, hold)
}

// RequestRestore durably claims the affected exports before acknowledging the
// request. Preparation and atomic publication then continue independently of
// the HTTP request lifetime and are observable through operation status.
func (service *Service) RequestRestore(ctx context.Context, scope RestoreScope, hold Hold) (string, RestoreClassification, error) {
	claim, err := service.repository.ClaimRestore(ctx, scope, service.now().UTC())
	if err != nil || claim.OperationID == "" {
		return "", claim.Classification, err
	}
	service.wg.Add(1)
	go func() {
		defer service.wg.Done()
		service.mu.Lock()
		defer service.mu.Unlock()
		_ = service.restoreClaim(service.ctx, claim, hold)
	}()
	return claim.OperationID, claim.Classification, nil
}

func (service *Service) restoreClaim(ctx context.Context, claim RestoreClaim, hold Hold) (restoreErr error) {
	segments := claim.Segments
	scope := claim.Scope
	claimFinished := false
	defer func() {
		if !claimFinished {
			failure := restoreErr
			if failure == nil {
				failure = errors.New("restore preparation did not complete")
			}
			_ = service.repository.FailRestore(context.Background(), claim.OperationID, failure, service.now().UTC())
		}
	}()
	publication := RestorePublication{OperationID: claim.OperationID}
	capacity, err := service.repository.RetentionCapacity(ctx, service.now().UTC())
	if err != nil {
		return err
	}
	var restorePeakBytes int64
	for _, segment := range segments {
		restorePeakBytes += segment.OriginalBytes * 2
	}
	if capacity.FilesystemAvailable && capacity.FilesystemFreeBytes < restorePeakBytes {
		return fmt.Errorf("%w: restore scope needs up to %d bytes, have %d", ErrInsufficientCapacity, restorePeakBytes, capacity.FilesystemFreeBytes)
	}
	keepReplacements := false
	defer func() {
		if keepReplacements {
			return
		}
		for _, replacement := range publication.Replacements {
			_ = service.archives.Remove(replacement.Segment.SegmentID)
		}
	}()
	for _, segment := range segments {
		verified, err := service.archives.OpenVerified(ctx, segment.ID)
		if err != nil {
			if errors.Is(err, ErrArchiveFormat) {
				return fmt.Errorf("verify restore segment %s: %w", segment.ID, err)
			}
			payload, metadata := segment.PayloadIntegrity, segment.MetadataIntegrity
			if errors.Is(err, ErrArchiveMetadata) {
				metadata = MetadataUnverifiable
			}
			if errors.Is(err, ErrArchivePayload) || !errors.Is(err, ErrArchiveMetadata) {
				payload = PayloadCorrupt
			}
			_ = service.repository.RecordSegmentVerificationFailure(context.Background(), segment.ID, payload, metadata, service.now().UTC(), err)
			return fmt.Errorf("verify restore segment %s: %w", segment.ID, err)
		}
		if verified.FileSHA256 != segment.FileSHA256 || verified.MembershipSHA256 != segment.MembershipSHA256 {
			failure := fmt.Errorf("%w: catalog digest mismatch", ErrArchiveMetadata)
			_ = service.repository.RecordSegmentVerificationFailure(context.Background(), segment.ID, segment.PayloadIntegrity, MetadataUnverifiable, service.now().UTC(), failure)
			return fmt.Errorf("verify restore segment %s: %w", segment.ID, failure)
		}
		publication.OriginalSegments = append(publication.OriginalSegments, segment.ID)
		var selected, remaining []RawExport
		for _, exported := range verified.Exports {
			matches := scope.Kind == RestoreBySegment || (!exported.ReceivedAt.Before(scope.Start) && exported.ReceivedAt.Before(scope.End))
			if matches {
				selected = append(selected, exported)
			} else {
				remaining = append(remaining, exported)
			}
		}
		if len(selected) == 0 {
			return fmt.Errorf("verify restore segment %s: %w: selected membership is missing", segment.ID, ErrInvalidArchive)
		}
		for _, exported := range selected {
			accepted, err := service.replayer.Replay(canonical.Signal(exported.Signal), ingest.Transport(exported.Transport), exported.ReceivedAt, exported.Protobuf, service.profiles)
			if err != nil {
				return fmt.Errorf("prepare restore export %d: %w", exported.ID, err)
			}
			accepted.Identity = ingest.RetainedIdentity{ID: exported.ID, PayloadOccurrence: exported.PayloadOccurrence}
			accepted.Journal.Harness.State = harness.ReceiptState(exported.HarnessState)
			accepted.Journal.Harness.Scope = exported.HarnessScope
			accepted.Journal.Harness.Fingerprint = exported.HarnessFingerprint
			accepted.Journal.Harness.Label = exported.HarnessLabel
			publication.Selected = append(publication.Selected, accepted)
		}
		if len(remaining) > 0 {
			installed, err := service.archives.BuildWithIncarnation(ctx, remaining, claim.OperationID+":"+segment.ID)
			if err != nil {
				return fmt.Errorf("build partial restore replacement: %w", err)
			}
			raw := make([]RawExport, len(remaining))
			for index, exported := range remaining {
				raw[index] = exported
			}
			publication.Replacements = append(publication.Replacements, SegmentReplacement{OriginalSegmentID: segment.ID,
				Segment: ArchivePublication{SegmentID: installed.SegmentID, FileName: installed.Path,
					FileSHA256: installed.FileSHA256, MembershipSHA256: installed.MembershipSHA256,
					StoredBytes: installed.StoredBytes, OriginalBytes: installed.OriginalBytes, Exports: raw}})
		}
	}
	if len(publication.Selected) == 0 {
		return nil
	}
	publication.Hold = hold
	if err := service.repository.PublishRestore(ctx, publication); err != nil {
		return err
	}
	claimFinished = true
	keepReplacements = true
	for _, segment := range segments {
		if err := service.removePublishedArchive(ctx, claim.OperationID, segment.ID); err != nil {
			return err
		}
	}
	return service.completePublishedRestore(ctx, claim.OperationID)
}

func (service *Service) removePublishedArchive(ctx context.Context, operationID, segmentID string) error {
	for {
		if err := service.archives.Remove(segmentID); err == nil {
			return nil
		} else {
			_ = service.repository.RecordRestoreRetry(context.Background(), operationID, fmt.Errorf("remove archive %s: %w", segmentID, err), service.now().UTC())
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("restore published; archive cleanup remains recoverable: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func (service *Service) completePublishedRestore(ctx context.Context, operationID string) error {
	for {
		if err := service.repository.CompleteRestore(ctx, operationID, service.now().UTC()); err == nil {
			return nil
		} else {
			_ = service.repository.RecordRestoreRetry(context.Background(), operationID, fmt.Errorf("complete restore: %w", err), service.now().UTC())
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("restore published; terminal publication remains recoverable: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func (service *Service) Close(ctx context.Context) error {
	service.cancel()
	done := make(chan struct{})
	go func() {
		service.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type Scheduler struct {
	cancel  context.CancelFunc
	done    chan struct{}
	service *Service
	wg      sync.WaitGroup
}

func StartScheduler(parent context.Context, service *Service) *Scheduler {
	return startScheduler(parent, service, 24*time.Hour)
}

func startScheduler(parent context.Context, service *Service, interval time.Duration) *Scheduler {
	ctx, cancel := context.WithCancel(parent)
	scheduler := &Scheduler{cancel: cancel, done: make(chan struct{}), service: service}
	launch := func() {
		scheduler.wg.Add(1)
		go func() {
			defer scheduler.wg.Done()
			_ = service.RunMaintenance(ctx)
		}()
	}
	go func() {
		defer close(scheduler.done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		launch()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				launch()
			}
		}
	}()
	return scheduler
}

func (scheduler *Scheduler) Close(ctx context.Context) error {
	scheduler.cancel()
	scheduler.service.cancel()
	select {
	case <-scheduler.done:
		done := make(chan struct{})
		go func() {
			scheduler.wg.Wait()
			close(done)
		}()
		select {
		case <-done:
			return scheduler.service.Close(ctx)
		case <-ctx.Done():
			return ctx.Err()
		}
	case <-ctx.Done():
		return ctx.Err()
	}
}
