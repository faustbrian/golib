package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/control"
	"github.com/jackc/pgx/v5"
)

var (
	// ErrNilQueryer reports a missing PostgreSQL read boundary.
	ErrNilQueryer = errors.New("postgres: queryer is nil")
	// ErrInvalidDesiredLookup reports an unscoped or unsupported target.
	ErrInvalidDesiredLookup = errors.New("postgres: invalid desired-state lookup")
	// ErrDesiredStateNotFound reports a target without durable desired state.
	ErrDesiredStateNotFound = errors.New("postgres: desired state not found")
)

type rowQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// DesiredStore reads tenant-scoped operational state for worker convergence.
type DesiredStore struct {
	queryer rowQueryer
}

// NewDesiredStore creates a desired-state read repository.
func NewDesiredStore(queryer rowQueryer) (*DesiredStore, error) {
	if queryer == nil {
		return nil, ErrNilQueryer
	}

	return &DesiredStore{queryer: queryer}, nil
}

// Get returns the latest durable desired state for one target.
func (s *DesiredStore) Get(
	ctx context.Context,
	tenant string,
	target controlplane.Target,
) (control.DesiredRecord, error) {
	if strings.TrimSpace(tenant) == "" ||
		strings.TrimSpace(target.Name) == "" ||
		!desiredTargetKind(target.Kind) {
		return control.DesiredRecord{}, ErrInvalidDesiredLookup
	}

	record, err := scanDesired(s.queryer.QueryRow(
		ctx,
		loadDesiredStateSQL,
		tenant,
		target.Kind,
		target.Name,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return control.DesiredRecord{}, ErrDesiredStateNotFound
	}
	if err != nil {
		return control.DesiredRecord{}, fmt.Errorf("postgres: get desired state: %w", err)
	}

	return record, nil
}

func desiredTargetKind(kind controlplane.TargetKind) bool {
	switch kind {
	case controlplane.TargetQueue,
		controlplane.TargetWorker,
		controlplane.TargetWorkerGroup:
		return true
	default:
		return false
	}
}

const loadDesiredStateSQL = `
SELECT tenant_id, target_kind, target_name, state, revision, command_id,
       changed_at
FROM queue_control_desired_states
WHERE tenant_id = $1 AND target_kind = $2 AND target_name = $3
`
