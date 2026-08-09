package workflow

import (
	"sort"
	"time"
)

// TimerProgressStatus identifies replayed durable timer progress.
type TimerProgressStatus uint8

const (
	// TimerWaiting has a persisted deadline and outstanding durable work.
	TimerWaiting TimerProgressStatus = 1
	// TimerFired has a persisted due-time observation.
	TimerFired TimerProgressStatus = 2
)

// TimerProgress is immutable state reconstructed only from persisted events.
type TimerProgress struct {
	stepName string
	status   TimerProgressStatus
	dueAt    time.Time
	firedAt  time.Time
}

// StepName returns the stable definition timer step.
func (progress TimerProgress) StepName() string { return progress.stepName }

// Status returns the durable timer state.
func (progress TimerProgress) Status() TimerProgressStatus { return progress.status }

// DueAt returns the persisted timer deadline.
func (progress TimerProgress) DueAt() time.Time { return progress.dueAt }

// FiredAt returns the persisted due observation, or zero while waiting.
func (progress TimerProgress) FiredAt() time.Time { return progress.firedAt }

// SignalProgress is one immutable durably accepted external signal.
type SignalProgress struct {
	stepName   string
	signalID   string
	receivedAt time.Time
	payload    []byte
}

// StepName returns the stable definition signal step.
func (progress SignalProgress) StepName() string { return progress.stepName }

// SignalID returns the inbound deduplication identity.
func (progress SignalProgress) SignalID() string { return progress.signalID }

// ReceivedAt returns the persisted acceptance time.
func (progress SignalProgress) ReceivedAt() time.Time { return progress.receivedAt }

// Payload returns an owned copy of the bounded signal payload.
func (progress SignalProgress) Payload() []byte { return cloneBytes(progress.payload) }

func validWaitEventFields(spec HistoryEventSpec) bool {
	if !stableName.MatchString(spec.StepName) || spec.Definition != (DefinitionReference{}) ||
		spec.SuccessorID != "" || spec.Attempt != 0 || spec.Code != "" || spec.Retryable {
		return false
	}
	switch spec.Kind {
	case EventTimerScheduled:
		return spec.IdempotencyKey == "" && spec.DueAt.After(spec.OccurredAt) && len(spec.Data) == 0
	case EventTimerFired:
		return spec.IdempotencyKey == "" && spec.DueAt.IsZero() && len(spec.Data) == 0
	default:
		return instanceIDPattern.MatchString(spec.IdempotencyKey) && spec.DueAt.IsZero()
	}
}

func (instance *Instance) applyWait(registry *Registry, event HistoryEvent) error {
	if event.kind != EventTimerScheduled && event.kind != EventTimerFired && event.kind != EventSignalReceived {
		return ErrInvalidTransition
	}
	definition, _ := registry.Resolve(instance.definition.Name(), instance.definition.Version())
	if event.kind == EventSignalReceived {
		step, ok := definitionStep(definition, event.stepName, StepSignal)
		_, exists := instance.signals[event.stepName]
		if !ok || (instance.status != StatusRunning && instance.status != StatusPaused) || exists ||
			len(event.data) > int(step.InputLimit) {
			return ErrInvalidTransition
		}
		instance.signals[event.stepName] = SignalProgress{
			stepName: event.stepName, signalID: event.idempotencyKey,
			receivedAt: event.occurredAt, payload: cloneBytes(event.data),
		}
		return nil
	}
	step, ok := definitionStep(definition, event.stepName, StepTimer)
	if !ok {
		return ErrInvalidTransition
	}
	progress, exists := instance.timers[event.stepName]
	if event.kind == EventTimerScheduled {
		if instance.status != StatusRunning || exists ||
			event.dueAt != canonicalTime(event.occurredAt.Add(step.Timeout)) {
			return ErrInvalidTransition
		}
		instance.timers[event.stepName] = TimerProgress{
			stepName: event.stepName, status: TimerWaiting, dueAt: event.dueAt,
		}
		return nil
	}
	if instance.status != StatusRunning || !exists || progress.status != TimerWaiting ||
		event.occurredAt.Before(progress.dueAt) {
		return ErrInvalidTransition
	}
	progress.status = TimerFired
	progress.firedAt = event.occurredAt
	instance.timers[event.stepName] = progress
	return nil
}

func definitionStep(definition Definition, name string, kind StepKind) (StepSpec, bool) {
	for _, step := range definition.spec.Steps {
		if step.Name == name && step.Kind == kind {
			return step, true
		}
	}
	return StepSpec{}, false
}

func sortedTimerProgress(progress map[string]TimerProgress) []TimerProgress {
	if len(progress) == 0 {
		return nil
	}
	names := sortedProgressNames(progress)
	result := make([]TimerProgress, 0, len(names))
	for _, name := range names {
		result = append(result, progress[name])
	}
	return result
}

func sortedSignalProgress(progress map[string]SignalProgress) []SignalProgress {
	if len(progress) == 0 {
		return nil
	}
	names := sortedProgressNames(progress)
	result := make([]SignalProgress, 0, len(names))
	for _, name := range names {
		result = append(result, cloneSignalProgress(progress[name]))
	}
	return result
}

func sortedProgressNames[T any](progress map[string]T) []string {
	names := make([]string, 0, len(progress))
	for name := range progress {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func cloneSignalProgress(progress SignalProgress) SignalProgress {
	progress.payload = cloneBytes(progress.payload)
	return progress
}

type timerProgressSnapshot struct {
	StepName string
	Status   TimerProgressStatus
	DueAt    time.Time
	FiredAt  time.Time
}

func timerProgressSnapshots(progress map[string]TimerProgress) []timerProgressSnapshot {
	timers := sortedTimerProgress(progress)
	if timers == nil {
		return nil
	}
	result := make([]timerProgressSnapshot, 0, len(timers))
	for _, timer := range timers {
		result = append(result, timerProgressSnapshot{
			StepName: timer.stepName, Status: timer.status, DueAt: timer.dueAt, FiredAt: timer.firedAt,
		})
	}
	return result
}

type signalProgressSnapshot struct {
	StepName   string
	SignalID   string
	ReceivedAt time.Time
	Payload    []byte
}

func signalProgressSnapshots(progress map[string]SignalProgress) []signalProgressSnapshot {
	signals := sortedSignalProgress(progress)
	if signals == nil {
		return nil
	}
	result := make([]signalProgressSnapshot, 0, len(signals))
	for _, signal := range signals {
		result = append(result, signalProgressSnapshot{
			StepName: signal.stepName, SignalID: signal.signalID,
			ReceivedAt: signal.receivedAt, Payload: signal.payload,
		})
	}
	return result
}
