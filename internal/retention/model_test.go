package retention

import (
	"errors"
	"testing"
	"time"
)

func TestPolicyPartitions(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name              string
		archive, deletion int
		wantErr           bool
	}{
		{name: "minimum", archive: 1, deletion: 2},
		{name: "maximum", archive: MaxDays - 1, deletion: MaxDays},
		{name: "archive below", archive: 0, deletion: 2, wantErr: true},
		{name: "deletion above", archive: 1, deletion: MaxDays + 1, wantErr: true},
		{name: "equal", archive: 7, deletion: 7, wantErr: true},
		{name: "reverse", archive: 8, deletion: 7, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewPolicy(test.archive, test.deletion, 4)
			if (err != nil) != test.wantErr {
				t.Fatalf("NewPolicy() error = %v, want error %v", err, test.wantErr)
			}
			if test.wantErr && !errors.Is(err, ErrInvalidPolicy) {
				t.Fatalf("NewPolicy() error = %v, want ErrInvalidPolicy", err)
			}
		})
	}
}

func TestEvaluateUsesFixedReceiveAgeAndHoldBoundary(t *testing.T) {
	t.Parallel()
	policy, err := NewPolicy(7, 30, 1)
	if err != nil {
		t.Fatal(err)
	}
	evaluatedAt := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	receivedAt := evaluatedAt.Add(-7 * Day)
	holdUntil := evaluatedAt
	if got := Evaluate(policy, ExportDisposition{State: StateActive, ReceivedAt: receivedAt, HoldUntil: &holdUntil}, evaluatedAt); got != EligibilityArchive {
		t.Fatalf("Evaluate() at expired hold = %q, want archive", got)
	}
	holdUntil = evaluatedAt.Add(time.Nanosecond)
	if got := Evaluate(policy, ExportDisposition{State: StateActive, ReceivedAt: receivedAt, HoldUntil: &holdUntil}, evaluatedAt); got != EligibilityNone {
		t.Fatalf("Evaluate() during hold = %q, want none", got)
	}
	if got := Evaluate(policy, ExportDisposition{State: StateArchived, ReceivedAt: evaluatedAt.Add(-30 * Day)}, evaluatedAt); got != EligibilityDelete {
		t.Fatalf("Evaluate() at deletion boundary = %q, want delete", got)
	}
	if got := Evaluate(policy, ExportDisposition{State: StateArchived, ReceivedAt: evaluatedAt.Add(-31 * Day), Restoring: true}, evaluatedAt); got != EligibilityNone {
		t.Fatalf("Evaluate() restoring = %q, want none", got)
	}
}

func TestRestoreScopeAndHoldBoundaries(t *testing.T) {
	t.Parallel()
	if _, err := SegmentScope(" "); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("SegmentScope() error = %v", err)
	}
	now := time.Now()
	if _, err := PeriodScope(now, now); !errors.Is(err, ErrInvalidScope) {
		t.Fatalf("PeriodScope() error = %v", err)
	}
	for _, days := range []int{MinDays, MaxDays} {
		hold, err := NewHold(days)
		if err != nil {
			t.Fatal(err)
		}
		if got := hold.Until(now); !got.Equal(now.UTC().Add(time.Duration(days) * Day)) {
			t.Fatalf("Hold.Until() = %v", got)
		}
	}
}

func TestAggregateCycle(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		children []OperationStatus
		want     CycleStatus
	}{
		{name: "zero work", want: CycleCompleted},
		{name: "running child", children: []OperationStatus{OperationCompleted, OperationRunning}, want: CycleRunning},
		{name: "failed after terminal", children: []OperationStatus{OperationCompleted, OperationFailed}, want: CycleFailed},
		{name: "all cancelled", children: []OperationStatus{OperationCancelled, OperationCancelled}, want: CycleCancelled},
		{name: "mixed success and cancellation", children: []OperationStatus{OperationCompleted, OperationCancelled}, want: CycleCompleted},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := AggregateCycle(test.children); got != test.want {
				t.Fatalf("AggregateCycle() = %q, want %q", got, test.want)
			}
		})
	}
}
