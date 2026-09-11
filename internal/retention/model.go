// Package retention owns Agentmetry's post-admission telemetry lifecycle rules.
package retention

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kotokumu/agentmetry/internal/ingest"
)

type RawExport struct {
	ID                  int64
	PayloadOccurrence   int64
	ReceivedAt          time.Time
	Signal              string
	Transport           string
	Source              string
	NormalizerVersion   int
	NormalizationStatus string
	NormalizationError  string
	HarnessState        string
	HarnessScope        string
	HarnessFingerprint  string
	HarnessLabel        string
	Protobuf            []byte
}

type ArchivePublication struct {
	OperationID      string
	CycleID          string
	SegmentID        string
	FileName         string
	FileSHA256       string
	MembershipSHA256 string
	StoredBytes      int64
	OriginalBytes    int64
	Exports          []RawExport
	EvaluatedAt      time.Time
	PolicyRevision   int64
}

type Segment struct {
	ID                  string
	ReferenceState      SegmentReferenceState
	FileName            string
	PayloadIntegrity    PayloadIntegrity
	MetadataIntegrity   MetadataIntegrity
	MinReceivedAt       time.Time
	MaxReceivedAt       time.Time
	ExportCount         int
	OriginalBytes       int64
	StoredBytes         int64
	FileSHA256          string
	MembershipSHA256    string
	VerifiedAt          time.Time
	CreatedAt           time.Time
	ScheduledDeleteAt   *time.Time
	ScheduleUnavailable string
	IntegrityError      string
}

type SegmentReplacement struct {
	OriginalSegmentID string
	Segment           ArchivePublication
}

type RestorePublication struct {
	OperationID      string
	Selected         []ingest.AcceptedExport
	OriginalSegments []string
	Replacements     []SegmentReplacement
	Hold             Hold
}

type Operation struct {
	ID               string
	Kind             OperationKind
	Status           OperationStatus
	Phase            string
	RequestedAt      time.Time
	EvaluatedAt      *time.Time
	AffectedExports  int64
	AffectedSegments int64
	CompletedAt      *time.Time
	Error            string
}

type Cycle struct {
	ID                      string
	Status                  CycleStatus
	StartedAt               time.Time
	EvaluatedAt             time.Time
	CohortSize              int64
	CohortUnavailableReason string
	PendingChildren         int64
	RunningChildren         int64
	CompletedChildren       int64
	FailedChildren          int64
	CancelledChildren       int64
	CompletedAt             *time.Time
	Error                   string
}

type CapacityReport struct {
	ObservedAt                time.Time
	TotalAllocatedBytes       int64
	DatabaseBytes             int64
	DatabaseUnusedBytes       int64
	WALBytes                  int64
	ArchiveBytes              int64
	ArchiveAllocatedBytes     int64
	StagingAllocatedBytes     int64
	ActiveRawBytes            int64
	ObservationBytes          int64
	QueryProjectionBytes      int64
	EstimatedArchivePeakBytes int64
	EstimatedRestorePeakBytes int64
	FilesystemFreeBytes       int64
	FilesystemAvailable       bool
	UnavailableReason         string
	Warning                   string
}

const (
	MinDays = 1
	MaxDays = 36_500
	Day     = 24 * time.Hour
)

var (
	ErrInvalidPolicy        = errors.New("invalid retention policy")
	ErrInvalidHold          = errors.New("invalid retention hold")
	ErrInvalidScope         = errors.New("invalid restore scope")
	ErrInsufficientCapacity = errors.New("insufficient storage capacity")
	ErrRestoreConflict      = errors.New("restore conflicts with another retention operation")
	ErrInvalidArchive       = errors.New("invalid archive segment")
	ErrArchivePayload       = errors.New("archive payload is corrupt or unreadable")
	ErrArchiveMetadata      = errors.New("archive lifecycle metadata is unverifiable")
	ErrArchiveFormat        = errors.New("unsupported archive format")
	ErrRetentionDisabled    = errors.New("retention policy is disabled")
)

type ArchiveInstall struct {
	SegmentID        string
	Path             string
	FileSHA256       string
	MembershipSHA256 string
	StoredBytes      int64
	OriginalBytes    int64
	ExportCount      int
}

type VerifiedArchiveMember struct {
	ID         int64
	ReceivedAt string
}

type VerifiedArchive struct {
	MembershipSHA256 string
	Members          []VerifiedArchiveMember
	Exports          []RawExport
	FileSHA256       string
	StoredBytes      int64
}

type DeletionPresence string

const (
	DeletionInstalled DeletionPresence = "installed"
	DeletionStaged    DeletionPresence = "staged"
	DeletionMissing   DeletionPresence = "missing"
)

type DeletionHandle interface {
	StageAndSync() error
	RemoveAndSync() error
	RestoreAndSync() error
	Inspect() (DeletionPresence, error)
}

type RestoreClaim struct {
	OperationID    string
	Segments       []Segment
	Scope          RestoreScope
	Classification RestoreClassification
}

type ArchiveClaim struct {
	OperationID string
	Exports     []RawExport
}

type RestoreClassification struct {
	Result            string
	CurrentSegmentIDs []string
	ActiveMatches     int64
	ArchivedMatches   int64
	DeletedMatches    int64
}

type Days int

func NewDays(value int) (Days, error) {
	if value < MinDays || value > MaxDays {
		return 0, fmt.Errorf("days must be between %d and %d", MinDays, MaxDays)
	}
	return Days(value), nil
}

func (days Days) Duration() time.Duration {
	return time.Duration(days) * Day
}

type Policy struct {
	Enabled      bool
	ArchiveAfter Days
	DeleteAfter  Days
	Revision     int64
}

func DisabledPolicy(revision int64) Policy {
	return Policy{Revision: revision}
}

func NewPolicy(archiveDays, deleteDays int, revision int64) (Policy, error) {
	archiveAfter, err := NewDays(archiveDays)
	if err != nil {
		return Policy{}, fmt.Errorf("%w: archive cutoff: %v", ErrInvalidPolicy, err)
	}
	deleteAfter, err := NewDays(deleteDays)
	if err != nil {
		return Policy{}, fmt.Errorf("%w: deletion cutoff: %v", ErrInvalidPolicy, err)
	}
	if deleteAfter <= archiveAfter {
		return Policy{}, fmt.Errorf("%w: deletion cutoff must be later than archive cutoff", ErrInvalidPolicy)
	}
	return Policy{Enabled: true, ArchiveAfter: archiveAfter, DeleteAfter: deleteAfter, Revision: revision}, nil
}

type Hold struct {
	Days Days
}

func NewHold(days int) (Hold, error) {
	value, err := NewDays(days)
	if err != nil {
		return Hold{}, fmt.Errorf("%w: %v", ErrInvalidHold, err)
	}
	return Hold{Days: value}, nil
}

func (hold Hold) Until(publishedAt time.Time) time.Time {
	return publishedAt.UTC().Add(hold.Days.Duration())
}

type State string

const (
	StateActive   State = "active"
	StateArchived State = "archived"
	StateDeleted  State = "deleted"
)

func (state State) Valid() bool {
	return state == StateActive || state == StateArchived || state == StateDeleted
}

type Eligibility string

const (
	EligibilityNone    Eligibility = "none"
	EligibilityArchive Eligibility = "archive"
	EligibilityDelete  Eligibility = "delete"
)

type ExportDisposition struct {
	State        State
	ReceivedAt   time.Time
	HoldUntil    *time.Time
	Restoring    bool
	ArchivedIn   string
	PayloadGood  bool
	MetadataGood bool
}

func Evaluate(policy Policy, disposition ExportDisposition, evaluatedAt time.Time) Eligibility {
	if !policy.Enabled || !disposition.State.Valid() || disposition.Restoring {
		return EligibilityNone
	}
	evaluatedAt = evaluatedAt.UTC()
	if disposition.State == StateActive {
		if disposition.HoldUntil != nil && evaluatedAt.Before(disposition.HoldUntil.UTC()) {
			return EligibilityNone
		}
		if evaluatedAt.Sub(disposition.ReceivedAt.UTC()) >= policy.ArchiveAfter.Duration() {
			return EligibilityArchive
		}
		return EligibilityNone
	}
	if disposition.State == StateArchived && evaluatedAt.Sub(disposition.ReceivedAt.UTC()) >= policy.DeleteAfter.Duration() {
		return EligibilityDelete
	}
	return EligibilityNone
}

type RestoreScopeKind string

const (
	RestoreBySegment RestoreScopeKind = "segment"
	RestoreByPeriod  RestoreScopeKind = "period"
)

type RestoreScope struct {
	Kind      RestoreScopeKind
	SegmentID string
	Start     time.Time
	End       time.Time
}

func SegmentScope(segmentID string) (RestoreScope, error) {
	segmentID = strings.TrimSpace(segmentID)
	if segmentID == "" {
		return RestoreScope{}, fmt.Errorf("%w: segment identity is required", ErrInvalidScope)
	}
	return RestoreScope{Kind: RestoreBySegment, SegmentID: segmentID}, nil
}

func PeriodScope(start, end time.Time) (RestoreScope, error) {
	start, end = start.UTC(), end.UTC()
	if start.IsZero() || end.IsZero() || !start.Before(end) {
		return RestoreScope{}, fmt.Errorf("%w: period must satisfy start < end", ErrInvalidScope)
	}
	return RestoreScope{Kind: RestoreByPeriod, Start: start, End: end}, nil
}

type SegmentReferenceState string

const (
	SegmentCurrent    SegmentReferenceState = "current"
	SegmentSuperseded SegmentReferenceState = "superseded"
	SegmentRestored   SegmentReferenceState = "restored"
	SegmentDeleted    SegmentReferenceState = "deleted"
)

type PayloadIntegrity string

const (
	PayloadIntact  PayloadIntegrity = "intact"
	PayloadCorrupt PayloadIntegrity = "corrupt"
)

type MetadataIntegrity string

const (
	MetadataVerifiable   MetadataIntegrity = "verifiable"
	MetadataUnverifiable MetadataIntegrity = "unverifiable"
)

type OperationKind string

const (
	OperationArchive OperationKind = "archive"
	OperationRestore OperationKind = "restore"
	OperationDelete  OperationKind = "delete"
)

type OperationStatus string

const (
	OperationPending   OperationStatus = "pending"
	OperationRunning   OperationStatus = "running"
	OperationCompleted OperationStatus = "completed"
	OperationFailed    OperationStatus = "failed"
	OperationCancelled OperationStatus = "cancelled"
)

func (status OperationStatus) Terminal() bool {
	return status == OperationCompleted || status == OperationFailed || status == OperationCancelled
}

type CycleStatus string

const (
	CycleRunning   CycleStatus = "running"
	CycleCompleted CycleStatus = "completed"
	CycleFailed    CycleStatus = "failed"
	CycleCancelled CycleStatus = "cancelled"
)

// AggregateCycle returns the parent state after successful cohort evaluation.
func AggregateCycle(children []OperationStatus) CycleStatus {
	if len(children) == 0 {
		return CycleCompleted
	}
	allCancelled := true
	for _, child := range children {
		if !child.Terminal() {
			return CycleRunning
		}
		if child == OperationFailed {
			return CycleFailed
		}
		allCancelled = allCancelled && child == OperationCancelled
	}
	if allCancelled {
		return CycleCancelled
	}
	return CycleCompleted
}
