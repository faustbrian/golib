package workflow

import "sort"

// ChildProgressStatus identifies replayed durable child-workflow progress.
type ChildProgressStatus uint8

const (
	// ChildScheduled has durable dispatch work but no terminal child outcome.
	ChildScheduled ChildProgressStatus = 1
	// ChildSucceeded is a known successful child terminal outcome.
	ChildSucceeded ChildProgressStatus = 2
	// ChildFailed is a known failed child terminal outcome.
	ChildFailed ChildProgressStatus = 3
)

// ChildProgress is immutable state reconstructed only from persisted events.
type ChildProgress struct {
	stepName   string
	childID    string
	definition DefinitionReference
	status     ChildProgressStatus
	input      []byte
	result     []byte
	code       string
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

func validChildEventFields(spec HistoryEventSpec) bool {
	if !stableName.MatchString(spec.StepName) || spec.Attempt != 0 ||
		spec.IdempotencyKey != "" || !spec.DueAt.IsZero() || spec.Retryable {
		return false
	}
	if spec.Kind == EventChildScheduled {
		return spec.Definition.valid() && spec.Code == ""
	}
	if spec.Definition != (DefinitionReference{}) {
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
	if !exists || progress.status != ChildScheduled || progress.childID != event.successorID ||
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
		})
	}
	return result
}
