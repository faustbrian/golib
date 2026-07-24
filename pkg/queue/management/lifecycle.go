package management

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"sync"
	"time"
)

const MaxLifecycleCommandResults = 10_000

var (
	// ErrInvalidWorkerLifecycleConfiguration reports incomplete target identity.
	ErrInvalidWorkerLifecycleConfiguration = errors.New("management: invalid worker lifecycle configuration")
	// ErrInvalidLifecycleCounter reports unbalanced admission or job accounting.
	ErrInvalidLifecycleCounter = errors.New("management: invalid lifecycle counter")
	// ErrDesiredStateTargetMismatch prevents state from crossing worker scopes.
	ErrDesiredStateTargetMismatch = errors.New("management: desired state target mismatch")
	// ErrInvalidDesiredStateTransition prevents unsupported target lifecycle state.
	ErrInvalidDesiredStateTransition = errors.New("management: invalid desired state transition")
	// ErrInvalidWorkerLifecycleContext reports a nil command context.
	ErrInvalidWorkerLifecycleContext = errors.New("management: invalid worker lifecycle context")
)

// WorkerLifecycleConfig identifies the queue admission scope controlled by one
// Queue instance and bounds its in-memory duplicate-command protection.
type WorkerLifecycleConfig struct {
	Metadata          StatusMetadata
	WorkerGroup       string
	Queue             string
	MaxCommandResults int
	Now               func() time.Time
}

// WorkerLifecycleSnapshot is one synchronized admission and drain view.
type WorkerLifecycleSnapshot struct {
	State       WorkerState
	DrainStatus DrainState
	CurrentJobs uint32
	Terminating bool
}

type lifecycleCommandEntry struct {
	command Command
	result  CommandResult
	done    chan struct{}
}

// WorkerLifecycle owns queue admission, in-flight job accounting, desired
// revisions, and bounded duplicate command results. It never touches a backend.
type WorkerLifecycle struct {
	applyMu sync.Mutex
	mu      sync.Mutex

	metadata    StatusMetadata
	workerGroup string
	queue       string
	capacity    int
	now         func() time.Time

	state         DesiredState
	admissions    uint32
	jobs          uint32
	drainTimedOut bool
	changed       chan struct{}
	records       map[Target]DesiredRecord
	commands      map[string]*lifecycleCommandEntry
}

// NewWorkerLifecycle creates an active queue lifecycle with bounded command
// history. Command history is never evicted; capacity exhaustion fails closed.
func NewWorkerLifecycle(config WorkerLifecycleConfig) (*WorkerLifecycle, error) {
	if config.Metadata.Validate() != nil || invalidIdentity(config.WorkerGroup) ||
		invalidIdentity(config.Queue) || config.MaxCommandResults < 1 ||
		config.MaxCommandResults > MaxLifecycleCommandResults || config.Now == nil {
		return nil, ErrInvalidWorkerLifecycleConfiguration
	}

	return &WorkerLifecycle{
		metadata: config.Metadata, workerGroup: config.WorkerGroup,
		queue: config.Queue, capacity: config.MaxCommandResults, now: config.Now,
		state: DesiredActive, changed: make(chan struct{}),
		records:  make(map[Target]DesiredRecord, 3),
		commands: make(map[string]*lifecycleCommandEntry, config.MaxCommandResults),
	}, nil
}

// BeginAdmission reserves one backend request when the lifecycle is active.
func (l *WorkerLifecycle) BeginAdmission() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.state != DesiredActive {
		return false
	}
	l.admissions++
	l.notifyLocked()

	return true
}

// EndAdmission releases a request that did not produce a runnable job.
func (l *WorkerLifecycle) EndAdmission() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.admissions == 0 {
		return ErrInvalidLifecycleCounter
	}
	l.admissions--
	l.notifyLocked()

	return nil
}

// PromoteAdmissionToJob atomically converts one request into in-flight work.
func (l *WorkerLifecycle) PromoteAdmissionToJob() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.admissions == 0 {
		return ErrInvalidLifecycleCounter
	}
	l.admissions--
	l.jobs++
	l.notifyLocked()

	return nil
}

// EndJob releases one in-flight job after handler settlement completes.
func (l *WorkerLifecycle) EndJob() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.jobs == 0 {
		return ErrInvalidLifecycleCounter
	}
	l.jobs--
	l.notifyLocked()

	return nil
}

// Snapshot returns synchronized worker lifecycle presentation state.
func (l *WorkerLifecycle) Snapshot() WorkerLifecycleSnapshot {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.snapshotLocked()
}

// ApplyDesiredState applies one durable revision and waits for the requested
// safe admission boundary. A canceled wait leaves the state active for retry.
func (l *WorkerLifecycle) ApplyDesiredState(
	ctx context.Context,
	record DesiredRecord,
) error {
	if ctx == nil {
		return ErrInvalidDesiredStateContext
	}
	if record.Validate() != nil {
		return ErrInvalidDesiredStateOutput
	}
	l.applyMu.Lock()
	defer l.applyMu.Unlock()
	if !l.matches(record.Target) {
		return ErrDesiredStateTargetMismatch
	}
	if !l.validTargetState(record.Target, record.State) {
		return ErrInvalidDesiredStateTransition
	}
	if current, exists := l.record(record.Target); exists {
		if record.Revision < current.Revision {
			return ErrDesiredStateRegression
		}
		if record.Revision == current.Revision {
			if record != current {
				return ErrDesiredStateConflict
			}

			return nil
		}
	}
	if err := l.transition(ctx, record.State); err != nil {
		return err
	}
	l.mu.Lock()
	l.records[record.Target] = record
	l.mu.Unlock()

	return nil
}

// Execute enforces lifecycle commands and returns a bounded correlated result.
// Failure/dead-letter and backend mutation actions remain unsupported.
func (l *WorkerLifecycle) Execute(
	ctx context.Context,
	command Command,
) (CommandResult, error) {
	if ctx == nil {
		return CommandResult{}, ErrInvalidWorkerLifecycleContext
	}
	if err := command.Validate(); err != nil {
		return CommandResult{}, err
	}
	entry, existing, result := l.commandEntry(command)
	if result != (CommandResult{}) {
		return result, nil
	}
	if existing {
		select {
		case <-entry.done:
			return entry.result, nil
		case <-ctx.Done():
			return l.result(command, CommandUnknown, ""), nil
		}
	}

	result = l.executeNew(ctx, command)
	l.mu.Lock()
	entry.result = result
	close(entry.done)
	l.mu.Unlock()

	return result, nil
}

// DecorateWorkerStatus overlays queue-owned admission state and capabilities
// on a native backend observation after verifying both describe one worker.
func (l *WorkerLifecycle) DecorateWorkerStatus(
	status WorkerStatus,
) (WorkerStatus, error) {
	if status.Validate() != nil || status.ID != l.metadata.ID ||
		status.Version != l.metadata.Version || status.Concurrency != l.metadata.Concurrency ||
		status.Protocol != l.metadata.Protocol || !containsIdentity(status.Queues, l.queue) {
		return WorkerStatus{}, ErrDesiredStateTargetMismatch
	}
	snapshot := l.Snapshot()
	status.State = snapshot.State
	status.DrainStatus = snapshot.DrainStatus
	status.CurrentJobs = snapshot.CurrentJobs
	capabilities := append([]Capability(nil), status.Capabilities...)
	capabilities = append(capabilities,
		CapabilityPause, CapabilityResume, CapabilityDrain, CapabilityTerminate,
	)
	sort.Slice(capabilities, func(i, j int) bool { return capabilities[i] < capabilities[j] })
	status.Capabilities = deduplicateCapabilities(capabilities)
	if status.Validate() != nil {
		return WorkerStatus{}, ErrInvalidStatusProviderOutput
	}

	return status, nil
}

func (l *WorkerLifecycle) commandEntry(
	command Command,
) (*lifecycleCommandEntry, bool, CommandResult) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if entry, exists := l.commands[command.IdempotencyKey]; exists {
		if !reflect.DeepEqual(entry.command, command) {
			return nil, false, l.result(command, CommandRejected, "idempotency_conflict")
		}

		return entry, true, CommandResult{}
	}
	if len(l.commands) >= l.capacity {
		return nil, false, l.result(command, CommandRejected, "idempotency_capacity")
	}
	entry := &lifecycleCommandEntry{command: command, done: make(chan struct{})}
	l.commands[command.IdempotencyKey] = entry

	return entry, false, CommandResult{}
}

func (l *WorkerLifecycle) executeNew(ctx context.Context, command Command) CommandResult {
	if command.Protocol != l.metadata.Protocol {
		return l.result(command, CommandUnsupported, "protocol_mismatch")
	}
	state, supported := lifecycleCommandState(command.Action)
	if !supported {
		return l.result(command, CommandUnsupported, "unsupported_action")
	}
	if !l.matches(command.Target) {
		return l.result(command, CommandRejected, "target_mismatch")
	}
	now := l.now().UTC()
	if !command.Deadline.After(now) {
		return l.result(command, CommandTimedOut, "deadline_exceeded")
	}
	commandContext, cancel := context.WithTimeout(ctx, command.Deadline.Sub(now))
	defer cancel()
	l.applyMu.Lock()
	err := l.transition(commandContext, state)
	l.applyMu.Unlock()
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return l.result(command, CommandTimedOut, "deadline_exceeded")
		}

		return l.result(command, CommandRejected, "invalid_transition")
	}

	return l.result(command, CommandAcknowledged, "")
}

func (l *WorkerLifecycle) transition(ctx context.Context, state DesiredState) error {
	l.mu.Lock()
	if l.state == DesiredTerminating && state != DesiredTerminating {
		l.mu.Unlock()
		return ErrInvalidDesiredStateTransition
	}
	l.state = state
	l.drainTimedOut = false
	l.notifyLocked()
	l.mu.Unlock()
	for {
		l.mu.Lock()
		complete := l.transitionCompleteLocked(state)
		changed := l.changed
		l.mu.Unlock()
		if complete {
			return nil
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				l.mu.Lock()
				l.drainTimedOut = true
				l.notifyLocked()
				l.mu.Unlock()
			}

			return ctx.Err()
		case <-changed:
		}
	}
}

func (l *WorkerLifecycle) transitionCompleteLocked(state DesiredState) bool {
	if state == DesiredActive {
		return true
	}
	if state == DesiredPaused {
		return l.admissions == 0
	}

	return l.admissions == 0 && l.jobs == 0
}

func (l *WorkerLifecycle) snapshotLocked() WorkerLifecycleSnapshot {
	snapshot := WorkerLifecycleSnapshot{CurrentJobs: l.jobs}
	switch l.state {
	case DesiredPaused:
		snapshot.State = WorkerPaused
		snapshot.DrainStatus = DrainNotRequested
	case DesiredDraining, DesiredTerminating:
		snapshot.State = WorkerDraining
		snapshot.Terminating = l.state == DesiredTerminating
		switch {
		case l.drainTimedOut:
			snapshot.DrainStatus = DrainTimedOut
		case l.admissions == 0 && l.jobs == 0:
			snapshot.DrainStatus = DrainCompleted
		default:
			snapshot.DrainStatus = DrainInProgress
		}
	default:
		snapshot.State = WorkerRunning
		snapshot.DrainStatus = DrainNotRequested
	}

	return snapshot
}

func (l *WorkerLifecycle) record(target Target) (DesiredRecord, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	record, exists := l.records[target]

	return record, exists
}

func (l *WorkerLifecycle) matches(target Target) bool {
	if target.Kind == TargetQueue {
		return target.Name == l.queue
	}
	if target.Kind == TargetWorker {
		return target.Name == l.metadata.ID
	}

	return target.Kind == TargetWorkerGroup && target.Name == l.workerGroup
}

func (*WorkerLifecycle) validTargetState(target Target, state DesiredState) bool {
	if target.Kind == TargetQueue {
		return state == DesiredActive || state == DesiredPaused
	}
	if target.Kind == TargetWorker {
		return state == DesiredDraining || state == DesiredTerminating
	}

	return target.Kind == TargetWorkerGroup && state.valid()
}

func (l *WorkerLifecycle) result(
	command Command,
	status CommandResultStatus,
	failureCode string,
) CommandResult {
	return CommandResult{
		CommandID: command.ID, IdempotencyKey: command.IdempotencyKey,
		WorkerID: l.metadata.ID, Protocol: l.metadata.Protocol,
		Status: status, FailureCode: failureCode, CompletedAt: l.now().UTC(),
	}
}

func (l *WorkerLifecycle) notifyLocked() {
	close(l.changed)
	l.changed = make(chan struct{})
}

func lifecycleCommandState(action CommandAction) (DesiredState, bool) {
	switch action {
	case CommandPause:
		return DesiredPaused, true
	case CommandResume:
		return DesiredActive, true
	case CommandDrain:
		return DesiredDraining, true
	case CommandTerminate:
		return DesiredTerminating, true
	default:
		return "", false
	}
}

func containsIdentity(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}

	return false
}

func deduplicateCapabilities(values []Capability) []Capability {
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}

	return result
}

var (
	_ Controller          = (*WorkerLifecycle)(nil)
	_ DesiredStateApplier = (*WorkerLifecycle)(nil)
)
