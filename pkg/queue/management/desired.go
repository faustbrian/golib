package management

import (
	"context"
	"errors"
	"sync"
	"time"
)

const MaxDesiredStateTargets = 256

var (
	// ErrDesiredStateNotFound reports a target without an authored state. A
	// worker keeps its current local state and does not infer an active state.
	ErrDesiredStateNotFound = errors.New("management: desired state not found")
	// ErrInvalidDesiredStateConfiguration reports an unsafe reconciler graph.
	ErrInvalidDesiredStateConfiguration = errors.New("management: invalid desired state configuration")
	// ErrInvalidDesiredStateContext reports a nil reconciliation context.
	ErrInvalidDesiredStateContext = errors.New("management: invalid desired state context")
	// ErrInvalidDesiredStateOutput reports malformed or mismatched source data.
	ErrInvalidDesiredStateOutput = errors.New("management: invalid desired state output")
	// ErrDesiredStateRegression prevents an older revision from replacing state.
	ErrDesiredStateRegression = errors.New("management: desired state revision regression")
	// ErrDesiredStateConflict prevents one revision from describing two states.
	ErrDesiredStateConflict = errors.New("management: desired state revision conflict")
)

// DesiredState is the durable lifecycle state authored by a control plane.
type DesiredState string

const (
	DesiredActive      DesiredState = "active"
	DesiredPaused      DesiredState = "paused"
	DesiredDraining    DesiredState = "draining"
	DesiredTerminating DesiredState = "terminating"
)

// DesiredRecord is one attributed, monotonic target-state revision.
type DesiredRecord struct {
	Target    Target
	State     DesiredState
	Revision  uint64
	ChangedAt time.Time
	CommandID string
}

// Validate rejects unauthored, unbounded, or unsupported desired state.
func (r DesiredRecord) Validate() error {
	if !desiredTarget(r.Target) {
		return invalid("target", "is unsupported or invalid")
	}
	if !r.State.valid() {
		return invalid("state", "is unsupported")
	}
	if r.Revision == 0 {
		return invalid("revision", "must be positive")
	}
	if r.ChangedAt.IsZero() {
		return invalid("changed_at", "is required")
	}
	if invalidIdentity(r.CommandID) {
		return invalid("command_id", "is required and must be bounded")
	}

	return nil
}

// DesiredStateReader reads authoritative tenant-scoped desired state.
type DesiredStateReader interface {
	GetDesiredState(context.Context, Target) (DesiredRecord, error)
}

// DesiredStateApplier changes local queue admission or lifecycle state.
type DesiredStateApplier interface {
	ApplyDesiredState(context.Context, DesiredRecord) error
}

// DesiredStateReconcilerConfig defines one bounded pull reconciliation set.
type DesiredStateReconcilerConfig struct {
	Reader  DesiredStateReader
	Applier DesiredStateApplier
	Targets []Target
}

// DesiredStateReconciler applies each revision at most once per target.
// Scheduling and retry policy remain caller-owned; Reconcile starts no
// goroutines and a source failure cannot stop normal queue delivery.
type DesiredStateReconciler struct {
	mu      sync.Mutex
	reader  DesiredStateReader
	applier DesiredStateApplier
	targets []Target
	applied map[Target]DesiredRecord
}

// NewDesiredStateReconciler creates a bounded revision-aware reconciler.
func NewDesiredStateReconciler(
	config DesiredStateReconcilerConfig,
) (*DesiredStateReconciler, error) {
	if nilStatusProvider(config.Reader) || nilStatusProvider(config.Applier) ||
		len(config.Targets) == 0 || len(config.Targets) > MaxDesiredStateTargets {
		return nil, ErrInvalidDesiredStateConfiguration
	}
	targets := append([]Target(nil), config.Targets...)
	seen := make(map[Target]struct{}, len(targets))
	for _, target := range targets {
		if !desiredTarget(target) {
			return nil, ErrInvalidDesiredStateConfiguration
		}
		if _, duplicate := seen[target]; duplicate {
			return nil, ErrInvalidDesiredStateConfiguration
		}
		seen[target] = struct{}{}
	}

	return &DesiredStateReconciler{
		reader: config.Reader, applier: config.Applier, targets: targets,
		applied: make(map[Target]DesiredRecord, len(targets)),
	}, nil
}

// Reconcile reads and applies every configured target in deterministic order.
// Successfully applied revisions remain committed when a later target fails.
func (r *DesiredStateReconciler) Reconcile(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidDesiredStateContext
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, target := range r.targets {
		record, err := r.reader.GetDesiredState(ctx, target)
		if errors.Is(err, ErrDesiredStateNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if record.Validate() != nil || record.Target != target {
			return ErrInvalidDesiredStateOutput
		}
		current, exists := r.applied[target]
		if exists && record.Revision < current.Revision {
			return ErrDesiredStateRegression
		}
		if exists && record.Revision == current.Revision {
			if record != current {
				return ErrDesiredStateConflict
			}
			continue
		}
		if err := r.applier.ApplyDesiredState(ctx, record); err != nil {
			return err
		}
		r.applied[target] = record
	}

	return nil
}

func desiredTarget(target Target) bool {
	if invalidIdentity(target.Name) {
		return false
	}
	switch target.Kind {
	case TargetQueue, TargetWorker, TargetWorkerGroup:
		return true
	default:
		return false
	}
}

func (s DesiredState) valid() bool {
	switch s {
	case DesiredActive, DesiredPaused, DesiredDraining, DesiredTerminating:
		return true
	default:
		return false
	}
}
