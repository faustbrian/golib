package workflow

import (
	"sort"
	"time"
)

// ActivityProgressStatus identifies replayed durable activity progress.
type ActivityProgressStatus uint8

const (
	// ActivityProgressReady is durable and eligible for attempt admission.
	ActivityProgressReady ActivityProgressStatus = 1
	// ActivityProgressRunning has one externally observable in-flight attempt.
	ActivityProgressRunning ActivityProgressStatus = 2
	// ActivityProgressSucceeded is a known successful terminal step outcome.
	ActivityProgressSucceeded ActivityProgressStatus = 3
	// ActivityProgressFailed is a known failed attempt awaiting policy action.
	ActivityProgressFailed ActivityProgressStatus = 4
	// ActivityProgressUnknown requires reconciliation or operator resolution.
	ActivityProgressUnknown ActivityProgressStatus = 5
	// ActivityProgressRetryWaiting has a persisted next-attempt admission time.
	ActivityProgressRetryWaiting ActivityProgressStatus = 6
)

// ActivityProgress is immutable state reconstructed only from persisted events.
type ActivityProgress struct {
	stepName       string
	status         ActivityProgressStatus
	attempt        uint32
	idempotencyKey string
	dueAt          time.Time
	input          []byte
	result         []byte
	code           string
	retryable      bool
}

// StepName returns the stable definition step name.
func (progress ActivityProgress) StepName() string { return progress.stepName }

// Status returns the durable activity progress state.
func (progress ActivityProgress) Status() ActivityProgressStatus { return progress.status }

// Attempt returns the latest one-based attempt, or zero before the first attempt.
func (progress ActivityProgress) Attempt() uint32 { return progress.attempt }

// IdempotencyKey returns the latest externally visible attempt key.
func (progress ActivityProgress) IdempotencyKey() string { return progress.idempotencyKey }

// DueAt returns the current attempt deadline or retry admission time.
func (progress ActivityProgress) DueAt() time.Time { return progress.dueAt }

// Input returns an owned copy of the durably scheduled activity input.
func (progress ActivityProgress) Input() []byte { return cloneBytes(progress.input) }

// Result returns an owned copy of result or safe failure details.
func (progress ActivityProgress) Result() []byte { return cloneBytes(progress.result) }

// Code returns the latest known-failure or unknown-outcome code.
func (progress ActivityProgress) Code() string { return progress.code }

// Retryable reports the persisted known-failure retry classification.
func (progress ActivityProgress) Retryable() bool { return progress.retryable }

func (instance *Instance) applyActivity(registry *Registry, event HistoryEvent) error {
	// Lifecycle replay already verified this pinned definition reference.
	definition, _ := registry.Resolve(instance.definition.Name(), instance.definition.Version())
	step, ok := definitionActivityStep(definition, event.stepName)
	if !ok {
		return ErrInvalidTransition
	}
	progress, exists := instance.activities[event.stepName]

	switch event.kind {
	case EventActivityScheduled:
		if instance.status != StatusRunning || exists || len(event.data) > int(step.InputLimit) {
			return ErrInvalidTransition
		}
		instance.activities[event.stepName] = ActivityProgress{
			stepName: event.stepName, status: ActivityProgressReady, input: cloneBytes(event.data),
		}
	case EventActivityAttemptStarted:
		if instance.status != StatusRunning ||
			(progress.status != ActivityProgressReady && progress.status != ActivityProgressRetryWaiting) ||
			event.attempt != progress.attempt+1 || event.attempt > step.Retry.MaxAttempts ||
			event.dueAt != canonicalTime(event.occurredAt.Add(step.Timeout)) ||
			(progress.status == ActivityProgressRetryWaiting && event.occurredAt.Before(progress.dueAt)) {
			return ErrInvalidTransition
		}
		progress.status = ActivityProgressRunning
		progress.attempt = event.attempt
		progress.idempotencyKey = event.idempotencyKey
		progress.dueAt = event.dueAt
		progress.result = nil
		progress.code = ""
		progress.retryable = false
		instance.activities[event.stepName] = progress
	case EventActivityAttemptSucceeded:
		if !activityOutcomeAllowed(instance.status, progress, exists, event) ||
			len(event.data) > int(step.ResultLimit) {
			return ErrInvalidTransition
		}
		progress.status = ActivityProgressSucceeded
		progress.dueAt = time.Time{}
		progress.result = cloneBytes(event.data)
		progress.code = ""
		progress.retryable = false
		instance.activities[event.stepName] = progress
	case EventActivityAttemptFailed:
		if !activityOutcomeAllowed(instance.status, progress, exists, event) ||
			len(event.data) > int(step.ResultLimit) {
			return ErrInvalidTransition
		}
		progress.status = ActivityProgressFailed
		progress.dueAt = time.Time{}
		progress.result = cloneBytes(event.data)
		progress.code = event.code
		progress.retryable = event.retryable
		instance.activities[event.stepName] = progress
	case EventActivityAttemptUnknown:
		if !activityOutcomeAllowed(instance.status, progress, exists, event) || len(event.data) > int(step.ResultLimit) {
			return ErrInvalidTransition
		}
		progress.status = ActivityProgressUnknown
		progress.dueAt = time.Time{}
		progress.result = cloneBytes(event.data)
		progress.code = event.code
		progress.retryable = false
		instance.activities[event.stepName] = progress
	case EventActivityRetryScheduled:
		if instance.status != StatusRunning || progress.status != ActivityProgressFailed ||
			!progress.retryable || progress.attempt >= step.Retry.MaxAttempts || event.attempt != progress.attempt ||
			event.dueAt != canonicalTime(event.occurredAt.Add(retryDelay(step.Retry, event.attempt))) {
			return ErrInvalidTransition
		}
		progress.status = ActivityProgressRetryWaiting
		progress.dueAt = event.dueAt
		instance.activities[event.stepName] = progress
	}
	return nil
}

func activityOutcomeAllowed(_ InstanceStatus, progress ActivityProgress, _ bool, event HistoryEvent) bool {
	return progress.status == ActivityProgressRunning && event.attempt == progress.attempt
}

func definitionActivityStep(definition Definition, name string) (StepSpec, bool) {
	for _, step := range definition.spec.Steps {
		if step.Name == name && step.Kind == StepActivity {
			return step, true
		}
	}
	return StepSpec{}, false
}

func retryDelay(policy RetryPolicy, attempt uint32) time.Duration {
	delay := policy.InitialDelay
	// Sixty-three doublings cover the complete positive time.Duration range,
	// keeping replay work bounded even for a very large persisted attempt.
	for range min(attempt-1, uint32(63)) {
		delay = time.Duration(min(uint64(delay)*2, uint64(policy.MaxDelay)))
	}
	return delay
}

func cloneActivityProgress(progress ActivityProgress) ActivityProgress {
	progress.input = cloneBytes(progress.input)
	progress.result = cloneBytes(progress.result)
	return progress
}

func sortedActivityProgress(activities map[string]ActivityProgress) []ActivityProgress {
	if len(activities) == 0 {
		return nil
	}
	names := make([]string, 0, len(activities))
	for name := range activities {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]ActivityProgress, 0, len(names))
	for _, name := range names {
		result = append(result, cloneActivityProgress(activities[name]))
	}
	return result
}

type activityProgressSnapshot struct {
	StepName       string
	Status         ActivityProgressStatus
	Attempt        uint32
	IdempotencyKey string
	DueAt          time.Time
	Input          []byte
	Result         []byte
	Code           string
	Retryable      bool
}

func activityProgressSnapshots(activities map[string]ActivityProgress) []activityProgressSnapshot {
	progresses := sortedActivityProgress(activities)
	if progresses == nil {
		return nil
	}
	snapshots := make([]activityProgressSnapshot, 0, len(progresses))
	for _, progress := range progresses {
		snapshots = append(snapshots, activityProgressSnapshot{
			StepName: progress.stepName, Status: progress.status, Attempt: progress.attempt,
			IdempotencyKey: progress.idempotencyKey, DueAt: progress.dueAt,
			Input: progress.input, Result: progress.result, Code: progress.code,
			Retryable: progress.retryable,
		})
	}
	return snapshots
}
