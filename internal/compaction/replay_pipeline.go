package compaction

import (
	"context"
	"fmt"

	"github.com/kotokumu/agentmetry/internal/ingest"
	source "github.com/kotokumu/agentmetry/sourceplugin"
)

// replayBatch is one non-empty, contiguous source prefix segment. items and
// exports have a one-to-one relationship and remain in legacy journal order.
type replayBatch struct {
	items   []replayItem
	exports []ingest.AcceptedExport
}

func newReplayBatch(items []replayItem) replayBatch {
	exports := make([]ingest.AcceptedExport, len(items))
	for index := range items {
		exports[index] = items[index].accepted
	}
	return replayBatch{items: items, exports: exports}
}

func (batch replayBatch) lastOrdinal() int64 {
	return batch.items[len(batch.items)-1].record.Ordinal
}

type replayBatchSource interface {
	NextReplayBatch(context.Context) (replayBatch, error)
}

type replayBatchCommitter interface {
	CommitReplayBatch(context.Context, []ingest.AcceptedExport) error
}

type replayPipeline struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func newReplayPipeline(parent context.Context) *replayPipeline {
	ctx, cancel := context.WithCancel(parent)
	return &replayPipeline{ctx: ctx, cancel: cancel}
}

func (pipeline *replayPipeline) Close() {
	pipeline.cancel()
}

type preparingReplaySource struct {
	chunks   replayChunkSource
	profiles source.Registry
}

func (source *preparingReplaySource) NextReplayBatch(ctx context.Context) (replayBatch, error) {
	if err := ctx.Err(); err != nil {
		return replayBatch{}, err
	}
	records, err := source.chunks.Next(ctx)
	if err != nil {
		return replayBatch{}, err
	}
	if len(records) == 0 {
		return replayBatch{}, nil
	}
	items, err := prepareReplayChunk(ctx, records, source.profiles)
	if err != nil {
		return replayBatch{}, err
	}
	return newReplayBatch(items), nil
}

// commitPreparedBatches overlaps preparation of the next source batch
// with the current durable commit. The unbuffered handoff bounds run-ahead to
// one preparing batch while the caller goroutine remains the only DB writer.
// A later preparation failure does not interrupt an earlier commit already in
// flight; that source prefix resolves first, then the private candidate fails.
func (pipeline *replayPipeline) commitPreparedBatches(
	source replayBatchSource,
	destination replayBatchCommitter,
	advanceDurablePrefix func(replayBatch) error,
) error {
	handoff := make(chan replayBatch)
	producerDone := make(chan error, 1)
	go func() {
		producerDone <- prepareReplayBatches(pipeline.ctx, source, handoff)
		close(handoff)
	}()

	var writeErr error
	for writeErr == nil {
		select {
		case <-pipeline.ctx.Done():
			writeErr = pipeline.ctx.Err()
		case batch, ok := <-handoff:
			if !ok {
				goto producerFinished
			}
			if err := pipeline.ctx.Err(); err != nil {
				writeErr = err
				break
			}
			if len(batch.items) == 0 || len(batch.items) != len(batch.exports) {
				writeErr = fmt.Errorf("commit replay batch: invalid item/export cardinality %d/%d", len(batch.items), len(batch.exports))
				break
			}
			if err := destination.CommitReplayBatch(pipeline.ctx, batch.exports); err != nil {
				writeErr = fmt.Errorf("write compact export batch ending at %d: %w", batch.lastOrdinal(), err)
				break
			}
			if advanceDurablePrefix != nil {
				if err := advanceDurablePrefix(batch); err != nil {
					writeErr = err
				}
			}
		}
	}

producerFinished:
	if writeErr != nil {
		pipeline.cancel()
	}
	producerErr := <-producerDone
	if writeErr != nil {
		return writeErr
	}
	return producerErr
}

func prepareReplayBatches(ctx context.Context, source replayBatchSource, handoff chan<- replayBatch) error {
	for {
		batch, err := source.NextReplayBatch(ctx)
		if err != nil {
			return err
		}
		if len(batch.items) == 0 {
			return nil
		}
		select {
		case handoff <- batch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
