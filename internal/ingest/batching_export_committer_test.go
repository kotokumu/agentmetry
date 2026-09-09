package ingest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type recordingExportBatchCommitter struct {
	calls   chan []AcceptedExport
	release chan struct{}
	err     error
	once    sync.Once
}

func (committer *recordingExportBatchCommitter) CommitExportBatch(ctx context.Context, exports []AcceptedExport) error {
	batch := append([]AcceptedExport(nil), exports...)
	select {
	case committer.calls <- batch:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-committer.release:
		return committer.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (committer *recordingExportBatchCommitter) unblock() {
	committer.once.Do(func() { close(committer.release) })
}

func Test_batchingPolicy_validate(t *testing.T) {
	type fields struct {
		windowExports    int
		windowBytes      int
		windowWait       time.Duration
		admissionExports int
		admissionBytes   int
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			name: "valid bounds",
			fields: fields{
				windowExports:    2,
				windowBytes:      16,
				windowWait:       time.Millisecond,
				admissionExports: 4,
				admissionBytes:   32,
			},
			wantErr: false,
		},
		{
			name: "window exceeds admission",
			fields: fields{
				windowExports:    5,
				windowBytes:      33,
				windowWait:       time.Millisecond,
				admissionExports: 4,
				admissionBytes:   32,
			},
			wantErr: true,
		},
		{
			name: "non-positive wait",
			fields: fields{
				windowExports:    2,
				windowBytes:      16,
				windowWait:       0,
				admissionExports: 4,
				admissionBytes:   32,
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := batchingPolicy{
				windowExports:    tt.fields.windowExports,
				windowBytes:      tt.fields.windowBytes,
				windowWait:       tt.fields.windowWait,
				admissionExports: tt.fields.admissionExports,
				admissionBytes:   tt.fields.admissionBytes,
			}
			if err := policy.validate(); (err != nil) != tt.wantErr {
				t.Errorf("batchingPolicy.validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBatchingExportCommitterBatchesByCountAndWaitsForDurableResult(t *testing.T) {
	destination := &recordingExportBatchCommitter{calls: make(chan []AcceptedExport, 1), release: make(chan struct{})}
	committer, err := newBatchingExportCommitter(destination, batchingPolicy{
		windowExports: 2, windowBytes: 1024, windowWait: time.Hour,
		admissionExports: 4, admissionBytes: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		destination.unblock()
		_ = committer.Close(context.Background())
	})

	results := make(chan error, 2)
	for _, payload := range [][]byte{{0x01}, {0x02}} {
		exported := AcceptedExport{Envelope: Envelope{Protobuf: payload}}
		go func() { results <- committer.CommitExport(context.Background(), exported) }()
	}

	select {
	case batch := <-destination.calls:
		if len(batch) != 2 {
			t.Fatalf("batch exports = %d, want 2", len(batch))
		}
	case <-time.After(time.Second):
		t.Fatal("count-complete window was not committed")
	}
	select {
	case err := <-results:
		t.Fatalf("CommitExport returned before durable result: %v", err)
	default:
	}
	destination.unblock()
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
}

func TestBatchingExportCommitterStartsANewWindowBeforeByteOverflow(t *testing.T) {
	destination := &recordingExportBatchCommitter{calls: make(chan []AcceptedExport, 2), release: make(chan struct{})}
	committer, err := newBatchingExportCommitter(destination, batchingPolicy{
		windowExports: 3, windowBytes: 3, windowWait: time.Hour,
		admissionExports: 4, admissionBytes: 8,
	})
	if err != nil {
		t.Fatal(err)
	}

	results := make(chan error, 2)
	for _, payload := range [][]byte{{0x01, 0x02}, {0x03, 0x04}} {
		exported := AcceptedExport{Envelope: Envelope{Protobuf: payload}}
		go func() { results <- committer.CommitExport(context.Background(), exported) }()
	}
	select {
	case batch := <-destination.calls:
		if len(batch) != 1 || len(batch[0].Envelope.Protobuf) != 2 {
			t.Fatalf("first byte-bounded batch = %#v, want one two-byte export", batch)
		}
	case <-time.After(time.Second):
		t.Fatal("would-overflow export did not close the first window")
	}
	destination.unblock()
	if err := committer.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case batch := <-destination.calls:
		if len(batch) != 1 || len(batch[0].Envelope.Protobuf) != 2 {
			t.Fatalf("second byte-bounded batch = %#v, want one two-byte export", batch)
		}
	default:
		t.Fatal("second export was not committed in its own window")
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
}

func TestBatchingExportCommitterReturnsTheBatchFailureToEveryCaller(t *testing.T) {
	wantErr := errors.New("disk full")
	destination := &recordingExportBatchCommitter{calls: make(chan []AcceptedExport, 1), release: make(chan struct{}), err: wantErr}
	committer, err := newBatchingExportCommitter(destination, batchingPolicy{
		windowExports: 2, windowBytes: 1024, windowWait: time.Hour,
		admissionExports: 4, admissionBytes: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}

	results := make(chan error, 2)
	for _, payload := range [][]byte{{0x01}, {0x02}} {
		exported := AcceptedExport{Envelope: Envelope{Protobuf: payload}}
		go func() { results <- committer.CommitExport(context.Background(), exported) }()
	}
	select {
	case <-destination.calls:
	case <-time.After(time.Second):
		t.Fatal("batch did not reach storage")
	}
	destination.unblock()
	for range 2 {
		if err := <-results; !errors.Is(err, wantErr) {
			t.Fatalf("CommitExport error = %v, want %v", err, wantErr)
		}
	}
	if err := committer.Close(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("Close error = %v, want persisted batch failure %v", err, wantErr)
	}
}

func TestBatchingExportCommitterFinishesAcceptedExportAfterCallerCancellation(t *testing.T) {
	destination := &recordingExportBatchCommitter{calls: make(chan []AcceptedExport, 1), release: make(chan struct{})}
	committer, err := newBatchingExportCommitter(destination, batchingPolicy{
		windowExports: 1, windowBytes: 1024, windowWait: time.Hour,
		admissionExports: 2, admissionBytes: 2048,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		destination.unblock()
		_ = committer.Close(context.Background())
	})

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- committer.CommitExport(ctx, AcceptedExport{Envelope: Envelope{Protobuf: []byte{0x01}}})
	}()
	select {
	case <-destination.calls:
	case <-time.After(time.Second):
		t.Fatal("accepted export did not reach storage")
	}
	cancel()
	select {
	case err := <-result:
		t.Fatalf("accepted export returned on caller cancellation: %v", err)
	default:
	}
	destination.unblock()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestBatchingExportCommitterShutdownDeadlineCancelsActiveCommit(t *testing.T) {
	destination := &recordingExportBatchCommitter{calls: make(chan []AcceptedExport, 1), release: make(chan struct{})}
	committer, err := newBatchingExportCommitter(destination, batchingPolicy{
		windowExports: 1, windowBytes: 1024, windowWait: time.Hour,
		admissionExports: 2, admissionBytes: 2048,
	})
	if err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	go func() {
		result <- committer.CommitExport(context.Background(), AcceptedExport{Envelope: Envelope{Protobuf: []byte{0x01}}})
	}()
	select {
	case <-destination.calls:
	case <-time.After(time.Second):
		t.Fatal("accepted export did not reach storage")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := committer.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close error = %v, want deadline exceeded", err)
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("CommitExport error = %v, want canceled", err)
	}
}

func TestBatchingExportCommitterFlushesOneExportAfterMaximumWait(t *testing.T) {
	destination := &recordingExportBatchCommitter{calls: make(chan []AcceptedExport, 1), release: make(chan struct{})}
	committer, err := newBatchingExportCommitter(destination, batchingPolicy{
		windowExports: 2, windowBytes: 1024, windowWait: 10 * time.Millisecond,
		admissionExports: 4, admissionBytes: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		destination.unblock()
		_ = committer.Close(context.Background())
	})

	result := make(chan error, 1)
	go func() {
		result <- committer.CommitExport(context.Background(), AcceptedExport{Envelope: Envelope{Protobuf: []byte{0x01}}})
	}()

	select {
	case batch := <-destination.calls:
		if len(batch) != 1 {
			t.Fatalf("batch exports = %d, want 1", len(batch))
		}
	case <-time.After(time.Second):
		t.Fatal("oldest export deadline did not flush the window")
	}
	destination.unblock()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestBatchingExportCommitterAppliesBackpressureUntilContextEnds(t *testing.T) {
	destination := &recordingExportBatchCommitter{calls: make(chan []AcceptedExport, 1), release: make(chan struct{})}
	committer, err := newBatchingExportCommitter(destination, batchingPolicy{
		windowExports: 1, windowBytes: 1024, windowWait: time.Hour,
		admissionExports: 1, admissionBytes: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		destination.unblock()
		_ = committer.Close(context.Background())
	})

	first := make(chan error, 1)
	go func() {
		first <- committer.CommitExport(context.Background(), AcceptedExport{Envelope: Envelope{Protobuf: []byte{0x01}}})
	}()
	select {
	case <-destination.calls:
	case <-time.After(time.Second):
		t.Fatal("first export did not reach storage")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err = committer.CommitExport(ctx, AcceptedExport{Envelope: Envelope{Protobuf: []byte{0x02}}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second CommitExport error = %v, want deadline exceeded", err)
	}
	destination.unblock()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
}

func TestBatchingExportCommitterCloseFlushesAcceptedWindow(t *testing.T) {
	destination := &recordingExportBatchCommitter{calls: make(chan []AcceptedExport, 1), release: make(chan struct{})}
	accepted := make(chan struct{})
	var acceptedOnce sync.Once
	committer, err := newBatchingExportCommitter(destination, batchingPolicy{
		windowExports: 2, windowBytes: 1024, windowWait: time.Hour,
		admissionExports: 4, admissionBytes: 4096,
	}, func() { acceptedOnce.Do(func() { close(accepted) }) })
	if err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	go func() {
		result <- committer.CommitExport(context.Background(), AcceptedExport{Envelope: Envelope{Protobuf: []byte{0x01}}})
	}()
	<-accepted
	closed := make(chan error, 1)
	go func() { closed <- committer.Close(context.Background()) }()

	select {
	case batch := <-destination.calls:
		if len(batch) != 1 {
			t.Fatalf("batch exports = %d, want 1", len(batch))
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not flush the accepted window")
	}
	destination.unblock()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if err := committer.CommitExport(context.Background(), AcceptedExport{}); err == nil {
		t.Fatal("CommitExport succeeded after Close")
	}
}
