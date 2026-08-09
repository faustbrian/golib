// Package workflow provides explicit durable workflow and saga primitives.
package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"
)

const (
	// MaxPayloadBytes bounds persisted workflow inputs and results.
	MaxPayloadBytes = 16 << 20
	// MaxFanOut bounds one definition's parallel admission request.
	MaxFanOut = 10_000
)

var stableName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/-]{0,127}$`)

var (
	// ErrInvalidDefinition classifies malformed or unsafe definitions.
	ErrInvalidDefinition = errors.New("invalid workflow definition")
	// ErrDuplicateDefinition classifies duplicate immutable definition keys.
	ErrDuplicateDefinition = errors.New("duplicate workflow definition")
	// ErrDefinitionNotFound classifies an unavailable pinned definition.
	ErrDefinitionNotFound = errors.New("workflow definition not found")
	// ErrInvalidMigration classifies incomplete or incoherent migrations.
	ErrInvalidMigration = errors.New("invalid workflow migration")
	// ErrDuplicateMigration classifies duplicate explicit migration edges.
	ErrDuplicateMigration = errors.New("duplicate workflow migration")
	// ErrMigrationNotFound classifies an unavailable explicit migration edge.
	ErrMigrationNotFound = errors.New("workflow migration not found")
)

// ExecutionMode distinguishes central orchestration from explicit external
// choreography. Choreography does not imply or install a global event bus.
type ExecutionMode uint8

const (
	// Orchestration advances through decisions made by one workflow definition.
	Orchestration ExecutionMode = 1
	// Choreography advances through explicitly supplied durable external events.
	Choreography ExecutionMode = 2
)

// StepKind identifies one durable definition step.
type StepKind uint8

const (
	// StepActivity requests an idempotent external activity.
	StepActivity StepKind = 1
	// StepSignal waits for a named external signal.
	StepSignal StepKind = 2
	// StepTimer waits for durable time to become due.
	StepTimer StepKind = 3
	// StepChild starts or observes a version-pinned child workflow.
	StepChild StepKind = 4
	// StepParallel admits a bounded set of branches.
	StepParallel StepKind = 5
	// StepJoin waits for an explicitly modeled branch set.
	StepJoin StepKind = 6
	// StepRace selects the first persisted winning branch.
	StepRace StepKind = 7
	// StepApproval waits for an authorized operator or application decision.
	StepApproval StepKind = 8
)

// RetryPolicy is a bounded retry contract. Its zero value is invalid.
type RetryPolicy struct {
	MaxAttempts  uint32
	InitialDelay time.Duration
	MaxDelay     time.Duration
}

// CompensationSpec defines an independently retryable compensating activity.
// A failed compensation remains a failed compensation; it is never translated
// into a successful rollback.
type CompensationSpec struct {
	Target      string
	Timeout     time.Duration
	ResultLimit uint32
	Retry       RetryPolicy
}

// StepSpec is definition input. NewDefinition validates and deep-copies it.
// Target names an activity, signal, child definition, or approval policy as
// selected by Kind.
type StepSpec struct {
	Name         string
	Kind         StepKind
	Target       string
	Timeout      time.Duration
	InputLimit   uint32
	ResultLimit  uint32
	Retry        RetryPolicy
	FanOutLimit  uint32
	Compensation *CompensationSpec
}

// DefinitionSpec supplies one stable immutable workflow definition version.
type DefinitionSpec struct {
	Name       string
	Version    string
	Mode       ExecutionMode
	Deprecated bool
	Steps      []StepSpec
}

// Definition is one validated immutable workflow behavior version.
// Its zero value is invalid.
type Definition struct {
	spec        DefinitionSpec
	fingerprint string
}

// NewDefinition validates, owns, and fingerprints one immutable definition.
func NewDefinition(spec DefinitionSpec) (Definition, error) {
	owned := cloneDefinitionSpec(spec)
	if err := validateDefinition(owned); err != nil {
		return Definition{}, err
	}

	// DefinitionSpec contains only JSON-total scalar, slice, and struct fields.
	encoded, _ := json.Marshal(owned)
	digest := sha256.Sum256(encoded)

	return Definition{
		spec:        owned,
		fingerprint: hex.EncodeToString(digest[:]),
	}, nil
}

// Name returns the stable definition name.
func (definition Definition) Name() string { return definition.spec.Name }

// Version returns the immutable behavior version.
func (definition Definition) Version() string { return definition.spec.Version }

// Mode returns the definition execution model.
func (definition Definition) Mode() ExecutionMode { return definition.spec.Mode }

// Deprecated reports whether new instance creation should be refused by a
// caller. Existing pinned instances remain resolvable.
func (definition Definition) Deprecated() bool { return definition.spec.Deprecated }

// Steps returns a deep copy of the ordered definition steps.
func (definition Definition) Steps() []StepSpec {
	return cloneSteps(definition.spec.Steps)
}

// Fingerprint returns the deterministic behavior digest used to detect silent
// reinterpretation of an immutable name and version.
func (definition Definition) Fingerprint() string { return definition.fingerprint }

// Reference returns the exact immutable behavior identity that instances must
// persist with their history.
func (definition Definition) Reference() DefinitionReference {
	return DefinitionReference{
		name:        definition.Name(),
		version:     definition.Version(),
		fingerprint: definition.Fingerprint(),
	}
}

func cloneDefinitionSpec(spec DefinitionSpec) DefinitionSpec {
	spec.Steps = cloneSteps(spec.Steps)
	return spec
}

func cloneSteps(steps []StepSpec) []StepSpec {
	if steps == nil {
		return nil
	}
	cloned := make([]StepSpec, len(steps))
	copy(cloned, steps)
	for index := range cloned {
		if steps[index].Compensation != nil {
			compensation := *steps[index].Compensation
			cloned[index].Compensation = &compensation
		}
	}
	return cloned
}

func validateDefinition(spec DefinitionSpec) error {
	if !stableName.MatchString(spec.Name) {
		return invalidDefinition("name")
	}
	if !stableName.MatchString(spec.Version) {
		return invalidDefinition("version")
	}
	if spec.Mode != Orchestration && spec.Mode != Choreography {
		return invalidDefinition("mode")
	}
	if len(spec.Steps) == 0 || len(spec.Steps) > MaxFanOut {
		return invalidDefinition("steps")
	}

	names := make(map[string]struct{}, len(spec.Steps))
	for _, step := range spec.Steps {
		if !stableName.MatchString(step.Name) {
			return invalidDefinition("step.name")
		}
		if _, exists := names[step.Name]; exists {
			return invalidDefinition("step.name")
		}
		names[step.Name] = struct{}{}
		if err := validateStep(step); err != nil {
			return err
		}
	}

	return nil
}

func validateStep(step StepSpec) error {
	switch step.Kind {
	case StepActivity, StepChild:
		if !stableName.MatchString(step.Target) || step.Timeout <= 0 ||
			!validPayloadLimit(step.InputLimit) || !validPayloadLimit(step.ResultLimit) ||
			!validRetry(step.Retry) {
			return invalidDefinition("step.activity")
		}
	case StepSignal, StepApproval:
		if !stableName.MatchString(step.Target) || step.Timeout <= 0 ||
			!validPayloadLimit(step.InputLimit) {
			return invalidDefinition("step.wait")
		}
	case StepTimer:
		if step.Timeout <= 0 {
			return invalidDefinition("step.timer")
		}
	case StepParallel, StepJoin, StepRace:
		if step.FanOutLimit == 0 || step.FanOutLimit > MaxFanOut {
			return invalidDefinition("step.fan_out")
		}
	default:
		return invalidDefinition("step.kind")
	}

	if step.Compensation != nil {
		compensation := step.Compensation
		if step.Kind != StepActivity || !stableName.MatchString(compensation.Target) ||
			compensation.Timeout <= 0 || !validPayloadLimit(compensation.ResultLimit) ||
			!validRetry(compensation.Retry) {
			return invalidDefinition("step.compensation")
		}
	}

	return nil
}

func validPayloadLimit(limit uint32) bool {
	return limit > 0 && limit <= MaxPayloadBytes
}

func validRetry(policy RetryPolicy) bool {
	return policy.MaxAttempts > 0 && policy.InitialDelay > 0 &&
		policy.MaxDelay >= policy.InitialDelay
}

func invalidDefinition(field string) error {
	return fmt.Errorf("%w: %s", ErrInvalidDefinition, field)
}
