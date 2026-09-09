package ingest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	defaultLiveWindowExports    = 256
	defaultLiveWindowBytes      = 32 << 20
	defaultLiveWindowWait       = 25 * time.Millisecond
	defaultLiveAdmissionExports = 512
	defaultLiveAdmissionBytes   = 64 << 20
)

type batchingPolicy struct {
	windowExports    int
	windowBytes      int
	windowWait       time.Duration
	admissionExports int
	admissionBytes   int
}

func (policy batchingPolicy) validate() error {
	if policy.windowExports <= 0 || policy.windowBytes <= 0 || policy.windowWait <= 0 {
		return fmt.Errorf("validate batching policy: window limits must be positive")
	}
	if policy.admissionExports < policy.windowExports || policy.admissionBytes < policy.windowBytes {
		return fmt.Errorf("validate batching policy: admission limits must cover one window")
	}
	return nil
}

type exportCommitRequest struct {
	export AcceptedExport
	bytes  int
	result chan error
}

type BatchingExportCommitter struct {
	destination ExportBatchCommitter
	policy      batchingPolicy

	lifecycleContext  context.Context
	cancelLifecycle   context.CancelFunc
	done              chan struct{}
	ready             chan struct{}
	closeOnce         sync.Once
	admissionObserver func()

	mu                sync.Mutex
	accepting         bool
	pending           []exportCommitRequest
	unresolvedExports int
	unresolvedBytes   int
	capacityChanged   chan struct{}
	commitError       error
}

func NewBatchingExportCommitter(destination ExportBatchCommitter) *BatchingExportCommitter {
	committer, err := newBatchingExportCommitter(destination, batchingPolicy{
		windowExports:    defaultLiveWindowExports,
		windowBytes:      defaultLiveWindowBytes,
		windowWait:       defaultLiveWindowWait,
		admissionExports: defaultLiveAdmissionExports,
		admissionBytes:   defaultLiveAdmissionBytes,
	})
	if err != nil {
		panic(err)
	}
	return committer
}

func newBatchingExportCommitter(destination ExportBatchCommitter, policy batchingPolicy, admissionObserver ...func()) (*BatchingExportCommitter, error) {
	if destination == nil {
		return nil, fmt.Errorf("create live export batcher: nil destination")
	}
	if len(admissionObserver) > 1 {
		return nil, fmt.Errorf("create live export batcher: multiple admission observers")
	}
	if err := policy.validate(); err != nil {
		return nil, err
	}
	lifecycleContext, cancelLifecycle := context.WithCancel(context.Background())
	committer := &BatchingExportCommitter{
		destination:      destination,
		policy:           policy,
		lifecycleContext: lifecycleContext,
		cancelLifecycle:  cancelLifecycle,
		done:             make(chan struct{}),
		ready:            make(chan struct{}, 1),
		accepting:        true,
		capacityChanged:  make(chan struct{}),
	}
	if len(admissionObserver) == 1 {
		committer.admissionObserver = admissionObserver[0]
	}
	go committer.run()
	return committer, nil
}

func (committer *BatchingExportCommitter) CommitExport(ctx context.Context, exported AcceptedExport) error {
	request := exportCommitRequest{
		export: exported,
		bytes:  len(exported.Envelope.Protobuf),
		result: make(chan error, 1),
	}
	if request.bytes > committer.policy.windowBytes || request.bytes > committer.policy.admissionBytes {
		return fmt.Errorf("admit live export: protobuf size %d exceeds batching limit", request.bytes)
	}
	for {
		committer.mu.Lock()
		if !committer.accepting {
			committer.mu.Unlock()
			return fmt.Errorf("admit live export: committer is closed")
		}
		if err := ctx.Err(); err != nil {
			committer.mu.Unlock()
			return err
		}
		fits := committer.unresolvedExports < committer.policy.admissionExports &&
			committer.unresolvedBytes+request.bytes <= committer.policy.admissionBytes
		if fits {
			committer.unresolvedExports++
			committer.unresolvedBytes += request.bytes
			committer.pending = append(committer.pending, request)
			committer.mu.Unlock()
			committer.signalReady()
			if committer.admissionObserver != nil {
				committer.admissionObserver()
			}
			return <-request.result
		}
		changed := committer.capacityChanged
		committer.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

func (committer *BatchingExportCommitter) Close(ctx context.Context) error {
	committer.closeOnce.Do(func() {
		committer.mu.Lock()
		committer.accepting = false
		committer.notifyCapacityLocked()
		committer.mu.Unlock()
		committer.signalReady()
	})
	select {
	case <-committer.done:
		return committer.closeError(nil)
	case <-ctx.Done():
		committer.cancelLifecycle()
		committer.signalReady()
		<-committer.done
		return committer.closeError(ctx.Err())
	}
}

func (committer *BatchingExportCommitter) run() {
	defer close(committer.done)
	defer committer.cancelLifecycle()
	var window []exportCommitRequest
	windowBytes := 0
	var timer *time.Timer
	var timerC <-chan time.Time
	stopTimer := func() {
		if timer == nil {
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer = nil
		timerC = nil
	}
	flush := func() {
		if len(window) == 0 {
			return
		}
		stopTimer()
		exports := make([]AcceptedExport, len(window))
		for index := range window {
			exports[index] = window[index].export
		}
		err := committer.lifecycleContext.Err()
		if err == nil {
			err = committer.destination.CommitExportBatch(committer.lifecycleContext, exports)
		}
		if err != nil {
			committer.mu.Lock()
			if committer.commitError == nil {
				committer.commitError = err
			}
			committer.mu.Unlock()
		}
		committer.resolve(window, err)
		window = nil
		windowBytes = 0
	}

	for {
		if err := committer.lifecycleContext.Err(); err != nil {
			committer.mu.Lock()
			queued := append([]exportCommitRequest(nil), committer.pending...)
			committer.pending = nil
			committer.mu.Unlock()
			window = append(window, queued...)
			committer.resolve(window, err)
			stopTimer()
			return
		}

		committer.mu.Lock()
		var next exportCommitRequest
		hasNext := len(committer.pending) > 0
		if hasNext {
			next = committer.pending[0]
			committer.pending = committer.pending[1:]
		}
		accepting := committer.accepting
		unresolved := committer.unresolvedExports
		committer.mu.Unlock()

		if hasNext {
			if len(window) > 0 && (len(window) >= committer.policy.windowExports || windowBytes+next.bytes > committer.policy.windowBytes) {
				flush()
			}
			window = append(window, next)
			windowBytes += next.bytes
			if len(window) == 1 {
				timer = time.NewTimer(committer.policy.windowWait)
				timerC = timer.C
			}
			if len(window) >= committer.policy.windowExports || windowBytes >= committer.policy.windowBytes {
				flush()
			}
			continue
		}

		if !accepting {
			flush()
			committer.mu.Lock()
			unresolved = committer.unresolvedExports
			committer.mu.Unlock()
			if unresolved == 0 {
				return
			}
		}

		select {
		case <-committer.ready:
		case <-timerC:
			timer = nil
			timerC = nil
			flush()
		case <-committer.lifecycleContext.Done():
		}
	}
}

func (committer *BatchingExportCommitter) resolve(requests []exportCommitRequest, err error) {
	if len(requests) == 0 {
		return
	}
	committer.mu.Lock()
	for _, request := range requests {
		committer.unresolvedExports--
		committer.unresolvedBytes -= request.bytes
	}
	committer.notifyCapacityLocked()
	committer.mu.Unlock()
	for _, request := range requests {
		request.result <- err
	}
}

func (committer *BatchingExportCommitter) signalReady() {
	select {
	case committer.ready <- struct{}{}:
	default:
	}
}

func (committer *BatchingExportCommitter) notifyCapacityLocked() {
	close(committer.capacityChanged)
	committer.capacityChanged = make(chan struct{})
}

func (committer *BatchingExportCommitter) closeError(err error) error {
	committer.mu.Lock()
	defer committer.mu.Unlock()
	return errors.Join(err, committer.commitError)
}
