package compaction

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/kotokumu/agentmetry/internal/ingest"
)

type replayBatchSourceFunc func(context.Context) (replayBatch, error)

func (function replayBatchSourceFunc) NextReplayBatch(ctx context.Context) (replayBatch, error) {
	return function(ctx)
}

func commitPreparedReplayBatches(
	ctx context.Context,
	source replayBatchSource,
	destination replayBatchCommitter,
	advanceDurablePrefix func(replayBatch) error,
) error {
	pipeline := newReplayPipeline(ctx)
	defer pipeline.Close()
	return pipeline.commitPreparedBatches(source, destination, advanceDurablePrefix)
}

type recordingReplayCommitter struct {
	mu           sync.Mutex
	committed    []int64
	active       int
	maximum      int
	firstStarted chan struct{}
	releaseFirst chan struct{}
	fail         error
}

func (committer *recordingReplayCommitter) CommitReplayBatch(ctx context.Context, exports []ingest.AcceptedExport) error {
	committer.mu.Lock()
	committer.active++
	if committer.active > committer.maximum {
		committer.maximum = committer.active
	}
	call := len(committer.committed) + 1
	committer.mu.Unlock()

	defer func() {
		committer.mu.Lock()
		committer.active--
		committer.mu.Unlock()
	}()
	if call == 1 && committer.firstStarted != nil {
		close(committer.firstStarted)
		select {
		case <-committer.releaseFirst:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if committer.fail != nil {
		return committer.fail
	}
	committer.mu.Lock()
	committer.committed = append(committer.committed, int64(exports[0].Journal.NormalizerVersion))
	committer.mu.Unlock()
	return nil
}

func TestCommitPreparedReplayBatchesOverlapsOneFollowingPreparationAndPreservesOrder(t *testing.T) {
	secondStarted := make(chan struct{})
	thirdStarted := make(chan struct{})
	next := 0
	source := replayBatchSourceFunc(func(context.Context) (replayBatch, error) {
		next++
		switch next {
		case 1:
			return replayBatchForTest(1), nil
		case 2:
			close(secondStarted)
			return replayBatchForTest(2), nil
		default:
			close(thirdStarted)
			return replayBatch{}, nil
		}
	})
	committer := &recordingReplayCommitter{
		firstStarted: make(chan struct{}),
		releaseFirst: make(chan struct{}),
	}
	var progressed []int64
	done := make(chan error, 1)
	go func() {
		done <- commitPreparedReplayBatches(context.Background(), source, committer, func(batch replayBatch) error {
			progressed = append(progressed, batch.lastOrdinal())
			return nil
		})
	}()

	waitForSignal(t, committer.firstStarted, "first commit")
	waitForSignal(t, secondStarted, "second batch preparation")
	if len(progressed) != 0 {
		t.Fatalf("progress advanced before the first durable commit: %v", progressed)
	}
	select {
	case <-thirdStarted:
		t.Fatal("pipeline prepared more than one batch ahead of the blocked writer")
	default:
	}
	close(committer.releaseFirst)
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	committer.mu.Lock()
	committed, maximum := append([]int64(nil), committer.committed...), committer.maximum
	committer.mu.Unlock()
	if len(committed) != 2 || committed[0] != 1 || committed[1] != 2 {
		t.Fatalf("committed batches = %v, want [1 2]", committed)
	}
	if maximum != 1 {
		t.Fatalf("concurrent commits = %d, want 1", maximum)
	}
	if len(progressed) != 2 || progressed[0] != 1 || progressed[1] != 2 {
		t.Fatalf("committed progress = %v, want [1 2]", progressed)
	}
}

func TestCommitPreparedReplayBatchesCancelsAndJoinsProducerAfterCommitFailure(t *testing.T) {
	wantErr := errors.New("disk full")
	producerStopped := make(chan struct{})
	next := 0
	source := replayBatchSourceFunc(func(ctx context.Context) (replayBatch, error) {
		next++
		if next == 1 {
			return replayBatchForTest(1), nil
		}
		<-ctx.Done()
		close(producerStopped)
		return replayBatch{}, ctx.Err()
	})
	committer := &recordingReplayCommitter{fail: wantErr}
	progressed := 0

	err := commitPreparedReplayBatches(context.Background(), source, committer, func(replayBatch) error {
		progressed++
		return nil
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want commit cause %v", err, wantErr)
	}
	if progressed != 0 {
		t.Fatalf("progress calls after failed commit = %d, want 0", progressed)
	}
	select {
	case <-producerStopped:
	default:
		t.Fatal("pipeline returned before the producer stopped")
	}
}

func TestCommitPreparedReplayBatchesCancelsProducerBlockedOnHandoff(t *testing.T) {
	wantErr := errors.New("disk full")
	secondPrepared := make(chan struct{})
	next := 0
	source := replayBatchSourceFunc(func(context.Context) (replayBatch, error) {
		next++
		switch next {
		case 1:
			return replayBatchForTest(1), nil
		case 2:
			close(secondPrepared)
			return replayBatchForTest(2), nil
		default:
			return replayBatch{}, nil
		}
	})
	committer := &recordingReplayCommitter{
		firstStarted: make(chan struct{}),
		releaseFirst: make(chan struct{}),
		fail:         wantErr,
	}
	done := make(chan error, 1)
	go func() {
		done <- commitPreparedReplayBatches(context.Background(), source, committer, nil)
	}()
	waitForSignal(t, committer.firstStarted, "first commit")
	waitForSignal(t, secondPrepared, "second prepared batch")
	close(committer.releaseFirst)
	select {
	case err := <-done:
		if !errors.Is(err, wantErr) {
			t.Fatalf("error = %v, want commit cause %v", err, wantErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("pipeline did not release the producer blocked on handoff")
	}
}

func TestCommitPreparedReplayBatchesStopsAfterDurablePrefixAccountingFailure(t *testing.T) {
	wantErr := errors.New("invalid replay expectation")
	secondPrepared := make(chan struct{})
	next := 0
	source := replayBatchSourceFunc(func(context.Context) (replayBatch, error) {
		next++
		switch next {
		case 1:
			return replayBatchForTest(1), nil
		case 2:
			close(secondPrepared)
			return replayBatchForTest(2), nil
		default:
			return replayBatch{}, nil
		}
	})
	committer := &recordingReplayCommitter{
		firstStarted: make(chan struct{}),
		releaseFirst: make(chan struct{}),
	}
	done := make(chan error, 1)
	go func() {
		done <- commitPreparedReplayBatches(context.Background(), source, committer, func(replayBatch) error {
			return wantErr
		})
	}()
	waitForSignal(t, committer.firstStarted, "first commit")
	waitForSignal(t, secondPrepared, "second prepared batch")
	close(committer.releaseFirst)
	if err := <-done; !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want accounting cause %v", err, wantErr)
	}
	committer.mu.Lock()
	committed := append([]int64(nil), committer.committed...)
	committer.mu.Unlock()
	if len(committed) != 1 || committed[0] != 1 {
		t.Fatalf("committed batches after accounting failure = %v, want [1]", committed)
	}
}

func TestCommitPreparedReplayBatchesDoesNotStartAnotherCommitAfterProgressCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	next := 0
	source := replayBatchSourceFunc(func(context.Context) (replayBatch, error) {
		next++
		if next <= 2 {
			return replayBatchForTest(int64(next)), nil
		}
		return replayBatch{}, nil
	})
	committer := &recordingReplayCommitter{}
	err := commitPreparedReplayBatches(ctx, source, committer, func(replayBatch) error {
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
	committer.mu.Lock()
	committed := append([]int64(nil), committer.committed...)
	committer.mu.Unlock()
	if len(committed) != 1 {
		t.Fatalf("committed batches after cancellation = %v, want one", committed)
	}
}

func TestCommitPreparedReplayBatchesLetsEarlierCommitResolveBeforeLaterPreparationError(t *testing.T) {
	wantErr := errors.New("corrupt source record")
	laterFailed := make(chan struct{})
	next := 0
	source := replayBatchSourceFunc(func(context.Context) (replayBatch, error) {
		next++
		if next == 1 {
			return replayBatchForTest(1), nil
		}
		close(laterFailed)
		return replayBatch{}, wantErr
	})
	committer := &recordingReplayCommitter{
		firstStarted: make(chan struct{}),
		releaseFirst: make(chan struct{}),
	}
	done := make(chan error, 1)
	go func() {
		done <- commitPreparedReplayBatches(context.Background(), source, committer, nil)
	}()
	waitForSignal(t, committer.firstStarted, "first commit")
	waitForSignal(t, laterFailed, "later preparation failure")
	select {
	case err := <-done:
		t.Fatalf("pipeline returned before the earlier commit resolved: %v", err)
	default:
	}
	close(committer.releaseFirst)
	if err := <-done; !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want preparation cause %v", err, wantErr)
	}
}

func replayBatchForTest(ordinal int64) replayBatch {
	return newReplayBatch([]replayItem{{
		record:   storedExport{Ordinal: ordinal},
		accepted: ingest.AcceptedExport{Journal: ingest.JournalMetadata{NormalizerVersion: int(ordinal)}},
	}})
}

func waitForSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}
