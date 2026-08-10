package workflow

import (
	"sort"
	"time"
)

// ChildProgressStatus identifies replayed durable child-workflow progress.
type ChildProgressStatus uint8

const (
	// ChildScheduled has durable dispatch work but no terminal child outcome.
	ChildScheduled ChildProgressStatus = 1
	// ChildSucceeded is a known successful child terminal outcome.
	ChildSucceeded ChildProgressStatus = 2
	// ChildFailed is a known failed child terminal outcome.
	ChildFailed ChildProgressStatus = 3
	// ChildStartRunning has one externally observable creation attempt.
	ChildStartRunning ChildProgressStatus = 4
	// ChildActive means the pinned child is known to exist and is nonterminal.
	ChildActive ChildProgressStatus = 5
	// ChildStartFailedStatus is a known-absent creation failure.
	ChildStartFailedStatus ChildProgressStatus = 6
	// ChildStartUnknownStatus requires reconciliation before redispatch.
	ChildStartUnknownStatus ChildProgressStatus = 7
	// ChildStartRetryWaiting has a persisted next-attempt admission time.
	ChildStartRetryWaiting ChildProgressStatus = 8
)

// ChildProgress is immutable state reconstructed only from persisted events.
type ChildProgress struct {
	stepName       string
	childID        string
	definition     DefinitionReference
	status         ChildProgressStatus
	input          []byte
	result         []byte
	code           string
	attempt        uint32
	idempotencyKey string
	dueAt          time.Time
	retryable      bool
}

// StepName returns the stable parent definition step.
func (progress ChildProgress) StepName() string { return progress.stepName }

// ChildID returns the stable child instance identity.
func (progress ChildProgress) ChildID() string { return progress.childID }

// Definition returns the exact pinned child behavior identity.
func (progress ChildProgress) Definition() DefinitionReference { return progress.definition }

// Status returns the replayed durable child state.
func (progress ChildProgress) Status() ChildProgressStatus { return progress.status }

// Input returns an owned copy of the persisted child input.
func (progress ChildProgress) Input() []byte { return cloneBytes(progress.input) }

// Result returns an owned copy of the known child terminal result.
func (progress ChildProgress) Result() []byte { return cloneBytes(progress.result) }

// Code returns the stable known-failure code, or empty for other states.
func (progress ChildProgress) Code() string { return progress.code }

// Attempt returns the latest one-based child-start attempt.
func (progress ChildProgress) Attempt() uint32 { return progress.attempt }

// IdempotencyKey returns the latest persisted child-start key.
func (progress ChildProgress) IdempotencyKey() string { return progress.idempotencyKey }

// DueAt returns the attempt deadline or retry admission time.
func (progress ChildProgress) DueAt() time.Time { return progress.dueAt }

// Retryable reports whether a known-absent creation failure permits retry.
func (progress ChildProgress) Retryable() bool { return progress.retryable }

func validChildEventFields(spec HistoryEventSpec) bool {
	if !stableName.MatchString(spec.StepName) {
		return false
	}
	if spec.Kind == EventChildScheduled {
		return spec.Definition.valid() && spec.Attempt == 0 && spec.IdempotencyKey == "" &&
			spec.DueAt.IsZero() && spec.Code == "" && !spec.Retryable
	}
	if spec.Definition != (DefinitionReference{}) {
		return false
	}
	switch spec.Kind {
	case EventChildStartAttempted:
		return spec.Attempt > 0 && instanceIDPattern.MatchString(spec.IdempotencyKey) &&
			spec.DueAt.After(spec.OccurredAt) && spec.Code == "" && !spec.Retryable && len(spec.Data) == 0
	case EventChildStarted:
		return spec.Attempt > 0 && spec.IdempotencyKey == "" && spec.DueAt.IsZero() &&
			spec.Code == "" && !spec.Retryable && len(spec.Data) == 0
	case EventChildStartFailed:
		return spec.Attempt > 0 && spec.IdempotencyKey == "" && spec.DueAt.IsZero() &&
			stableName.MatchString(spec.Code) && len(spec.Data) == 0
	case EventChildStartUnknown:
		return spec.Attempt > 0 && spec.IdempotencyKey == "" && spec.DueAt.IsZero() &&
			stableName.MatchString(spec.Code) && !spec.Retryable && len(spec.Data) == 0
	case EventChildStartRetryScheduled:
		return spec.Attempt > 0 && spec.IdempotencyKey == "" && spec.DueAt.After(spec.OccurredAt) &&
			spec.Code == "" && !spec.Retryable && len(spec.Data) == 0
	}
	if spec.Attempt != 0 || spec.IdempotencyKey != "" || !spec.DueAt.IsZero() || spec.Retryable {
		return false
	}
	if spec.Kind == EventChildCompleted {
		return spec.Code == ""
	}
	return stableName.MatchString(spec.Code)
}

func (instance *Instance) applyChild(registry *Registry, event HistoryEvent) error {
	definition, _ := registry.Resolve(instance.definition.Name(), instance.definition.Version())
	step, ok := definitionStep(definition, event.stepName, StepChild)
	progress, exists := instance.children[event.stepName]
	if !ok || instance.status != StatusRunning {
		return ErrInvalidTransition
	}
	if event.kind == EventChildScheduled {
		if exists || step.ChildDefinition != event.definition ||
			len(event.data) > int(step.InputLimit) || validateReference(registry, event.definition) != nil {
			return ErrInvalidTransition
		}
		instance.children[event.stepName] = ChildProgress{
			stepName: event.stepName, childID: event.successorID, definition: event.definition,
			status: ChildScheduled, input: cloneBytes(event.data),
		}
		return nil
	}
	if !exists || progress.childID != event.successorID {
		return ErrInvalidTransition
	}
	switch event.kind {
	case EventChildStartAttempted:
		if instance.status != StatusRunning ||
			(progress.status != ChildScheduled && progress.status != ChildStartRetryWaiting) ||
			event.attempt != progress.attempt+1 || event.attempt > step.Retry.MaxAttempts ||
			event.dueAt != canonicalTime(event.occurredAt.Add(step.Timeout)) ||
			(progress.status == ChildStartRetryWaiting && event.occurredAt.Before(progress.dueAt)) {
			return ErrInvalidTransition
		}
		progress.status = ChildStartRunning
		progress.attempt = event.attempt
		progress.idempotencyKey = event.idempotencyKey
		progress.dueAt = event.dueAt
		progress.code = ""
		progress.retryable = false
	case EventChildStarted:
		if progress.status != ChildStartRunning || event.attempt != progress.attempt {
			return ErrInvalidTransition
		}
		progress.status = ChildActive
		progress.dueAt = time.Time{}
	case EventChildStartFailed:
		if progress.status != ChildStartRunning || event.attempt != progress.attempt {
			return ErrInvalidTransition
		}
		progress.status = ChildStartFailedStatus
		progress.dueAt = time.Time{}
		progress.code = event.code
		progress.retryable = event.retryable
	case EventChildStartUnknown:
		if progress.status != ChildStartRunning || event.attempt != progress.attempt {
			return ErrInvalidTransition
		}
		progress.status = ChildStartUnknownStatus
		progress.dueAt = time.Time{}
		progress.code = event.code
		progress.retryable = false
	case EventChildStartRetryScheduled:
		if progress.status != ChildStartFailedStatus || !progress.retryable ||
			progress.attempt >= step.Retry.MaxAttempts || event.attempt != progress.attempt ||
			event.dueAt != canonicalTime(event.occurredAt.Add(retryDelay(step.Retry, event.attempt))) {
			return ErrInvalidTransition
		}
		progress.status = ChildStartRetryWaiting
		progress.dueAt = event.dueAt
	default:
		if (progress.status != ChildScheduled && progress.status != ChildActive) ||
			len(event.data) > int(step.ResultLimit) {
			return ErrInvalidTransition
		}
		progress.result = cloneBytes(event.data)
		progress.code = event.code
		if event.kind == EventChildCompleted {
			progress.status = ChildSucceeded
		} else {
			progress.status = ChildFailed
		}
	}
	instance.children[event.stepName] = progress
	return nil
}

func cloneChildProgress(progress ChildProgress) ChildProgress {
	progress.input = cloneBytes(progress.input)
	progress.result = cloneBytes(progress.result)
	return progress
}

func sortedChildProgress(progress map[string]ChildProgress) []ChildProgress {
	if len(progress) == 0 {
		return nil
	}
	names := make([]string, 0, len(progress))
	for name := range progress {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]ChildProgress, 0, len(names))
	for _, name := range names {
		result = append(result, cloneChildProgress(progress[name]))
	}
	return result
}

type childProgressSnapshot struct {
	StepName              string
	ChildID               string
	DefinitionName        string
	DefinitionVersion     string
	DefinitionFingerprint string
	Status                ChildProgressStatus
	Input                 []byte
	Result                []byte
	Code                  string
	Attempt               uint32
	IdempotencyKey        string
	DueAt                 time.Time
	Retryable             bool
}

func childProgressSnapshots(progress map[string]ChildProgress) []childProgressSnapshot {
	children := sortedChildProgress(progress)
	if children == nil {
		return nil
	}
	result := make([]childProgressSnapshot, 0, len(children))
	for _, child := range children {
		result = append(result, childProgressSnapshot{
			StepName: child.stepName, ChildID: child.childID,
			DefinitionName: child.definition.Name(), DefinitionVersion: child.definition.Version(),
			DefinitionFingerprint: child.definition.Fingerprint(), Status: child.status,
			Input: child.input, Result: child.result, Code: child.code,
			Attempt: child.attempt, IdempotencyKey: child.idempotencyKey,
			DueAt: child.dueAt, Retryable: child.retryable,
		})
	}
	return result
}
