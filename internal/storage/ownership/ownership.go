// Package ownership provides the cross-process ownership boundary shared by
// normal SQLite use and whole-database data migrations.
package ownership

import (
	"context"
	"fmt"
	"time"

	"github.com/gofrs/flock"
)

const retryDelay = 100 * time.Millisecond

// Lock is held for the complete lifetime of a database owner. The lock file is
// deliberately stable across database replacement.
type Lock struct {
	file *flock.Flock
}

func Acquire(ctx context.Context, databasePath string) (*Lock, error) {
	file := flock.New(databasePath+".lock", flock.SetPermissions(0o600))
	locked, err := file.TryLockContext(ctx, retryDelay)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("acquire database ownership: %w", err)
	}
	if !locked {
		_ = file.Close()
		return nil, fmt.Errorf("acquire database ownership: lock unavailable")
	}
	return &Lock{file: file}, nil
}

func (lock *Lock) Close() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	err := lock.file.Close()
	lock.file = nil
	if err != nil {
		return fmt.Errorf("release database ownership: %w", err)
	}
	return nil
}
