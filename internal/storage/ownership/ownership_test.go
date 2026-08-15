package ownership

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestAcquireExcludesAnotherDatabaseOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agentmetry.db")
	first, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Acquire(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("second acquisition error = %v, want context cancellation", err)
	}
}

func TestAcquireReleasesOwnership(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agentmetry.db")
	first, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}
