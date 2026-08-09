package workflow_test

import (
	"errors"
	"strconv"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func TestDefinitionOwnsImmutableVersionedBehavior(t *testing.T) {
	t.Parallel()

	steps := []workflow.StepSpec{
		{
			Name:        "reserve",
			Kind:        workflow.StepActivity,
			Target:      "inventory.reserve",
			Timeout:     time.Minute,
			InputLimit:  4 << 10,
			ResultLimit: 4 << 10,
			Retry: workflow.RetryPolicy{
				MaxAttempts:  3,
				InitialDelay: time.Second,
				MaxDelay:     time.Minute,
			},
			Compensation: &workflow.CompensationSpec{
				Target:      "inventory.release",
				Timeout:     time.Minute,
				ResultLimit: 4 << 10,
				Retry: workflow.RetryPolicy{
					MaxAttempts:  5,
					InitialDelay: time.Second,
					MaxDelay:     time.Minute,
				},
			},
		},
	}

	definition, err := workflow.NewDefinition(workflow.DefinitionSpec{
		Name:    "order.fulfillment",
		Version: "2026-08-09",
		Mode:    workflow.Orchestration,
		Steps:   steps,
	})
	if err != nil {
		t.Fatalf("construct definition: %v", err)
	}

	fingerprint := definition.Fingerprint()
	steps[0].Target = "inventory.corrupted"
	steps[0].Compensation.Target = "inventory.corrupted"

	got := definition.Steps()
	if got[0].Target != "inventory.reserve" {
		t.Fatalf("definition retained caller-owned target: %q", got[0].Target)
	}
	if got[0].Compensation.Target != "inventory.release" {
		t.Fatalf("definition retained caller-owned compensation: %q", got[0].Compensation.Target)
	}
	got[0].Target = "inventory.changed-again"
	if definition.Steps()[0].Target != "inventory.reserve" {
		t.Fatal("Steps returned mutable definition state")
	}
	if definition.Fingerprint() != fingerprint {
		t.Fatal("caller mutation changed the immutable definition fingerprint")
	}
	if definition.Name() != "order.fulfillment" || definition.Version() != "2026-08-09" {
		t.Fatalf("unexpected key: %s@%s", definition.Name(), definition.Version())
	}
	if definition.Mode() != workflow.Orchestration || definition.Deprecated() {
		t.Fatal("unexpected definition metadata")
	}
}

func TestDefinitionSupportsExplicitBoundedStepKinds(t *testing.T) {
	t.Parallel()

	activity := workflow.StepSpec{
		Name: "activity", Kind: workflow.StepActivity, Target: "work.execute",
		Timeout: time.Minute, InputLimit: 1, ResultLimit: 1,
		Retry: workflow.RetryPolicy{MaxAttempts: 1, InitialDelay: time.Second, MaxDelay: time.Second},
	}
	child := activity
	child.Name = "child"
	child.Kind = workflow.StepChild
	child.Target = "child.workflow"
	child.ChildDefinition = mustDefinition(t, "child.workflow", "1").Reference()

	definition, err := workflow.NewDefinition(workflow.DefinitionSpec{
		Name: "all.steps", Version: "1", Mode: workflow.Choreography, Deprecated: true,
		Steps: []workflow.StepSpec{
			activity,
			child,
			{Name: "signal", Kind: workflow.StepSignal, Target: "shipment.ready", Timeout: time.Hour, InputLimit: 1},
			{Name: "approval", Kind: workflow.StepApproval, Target: "finance.approval", Timeout: time.Hour, InputLimit: 1},
			{Name: "timer", Kind: workflow.StepTimer, Timeout: time.Second},
			{Name: "parallel", Kind: workflow.StepParallel, FanOutLimit: 1, Branches: []string{"activity"}},
			{Name: "join", Kind: workflow.StepJoin, FanOutLimit: 1, Branches: []string{"activity"}},
			{Name: "race", Kind: workflow.StepRace, FanOutLimit: 2, Branches: []string{"signal", "approval"}},
		},
	})
	if err != nil {
		t.Fatalf("construct all step kinds: %v", err)
	}
	if definition.Mode() != workflow.Choreography || !definition.Deprecated() {
		t.Fatal("definition did not preserve explicit mode and deprecation")
	}
	if len(definition.Steps()) != 8 {
		t.Fatalf("step count = %d", len(definition.Steps()))
	}
	if got := (workflow.Definition{}).Steps(); got != nil {
		t.Fatalf("zero definition steps = %#v", got)
	}
}

func TestDefinitionAcceptsEveryExactSafetyBoundary(t *testing.T) {
	t.Parallel()

	steps := make([]workflow.StepSpec, workflow.MaxFanOut)
	for index := range steps {
		steps[index] = workflow.StepSpec{
			Name:        "step-" + strconv.Itoa(index),
			Kind:        workflow.StepActivity,
			Target:      "work.execute",
			Timeout:     time.Nanosecond,
			InputLimit:  workflow.MaxPayloadBytes,
			ResultLimit: workflow.MaxPayloadBytes,
			Retry: workflow.RetryPolicy{
				MaxAttempts:  1,
				InitialDelay: time.Nanosecond,
				MaxDelay:     time.Nanosecond,
			},
		}
	}
	steps[0].Compensation = &workflow.CompensationSpec{
		Target: "work.undo", Timeout: time.Nanosecond,
		ResultLimit: workflow.MaxPayloadBytes,
		Retry: workflow.RetryPolicy{
			MaxAttempts: 1, InitialDelay: time.Nanosecond, MaxDelay: time.Nanosecond,
		},
	}
	branches := make([]string, 0, len(steps)-2)
	for _, step := range steps[2:] {
		branches = append(branches, step.Name)
	}
	steps[1] = workflow.StepSpec{
		Name: "parallel-boundary", Kind: workflow.StepParallel,
		FanOutLimit: workflow.MaxFanOut, Branches: branches,
	}

	if _, err := workflow.NewDefinition(workflow.DefinitionSpec{
		Name: "boundaries", Version: "1", Mode: workflow.Orchestration, Steps: steps,
	}); err != nil {
		t.Fatalf("exact safety boundary rejected: %v", err)
	}
}

func TestDefinitionRejectsUnsafeOrAmbiguousSteps(t *testing.T) {
	t.Parallel()

	validActivity := workflow.StepSpec{
		Name:        "charge",
		Kind:        workflow.StepActivity,
		Target:      "payments.charge",
		Timeout:     time.Minute,
		InputLimit:  1024,
		ResultLimit: 1024,
		Retry: workflow.RetryPolicy{
			MaxAttempts:  2,
			InitialDelay: time.Second,
			MaxDelay:     time.Minute,
		},
	}

	tests := map[string]func() workflow.DefinitionSpec{
		"missing steps": func() workflow.DefinitionSpec {
			return workflow.DefinitionSpec{Name: "payments", Version: "1", Mode: workflow.Orchestration}
		},
		"missing stable name": func() workflow.DefinitionSpec {
			return workflow.DefinitionSpec{Version: "1", Mode: workflow.Orchestration, Steps: []workflow.StepSpec{validActivity}}
		},
		"missing immutable version": func() workflow.DefinitionSpec {
			return workflow.DefinitionSpec{Name: "payments", Mode: workflow.Orchestration, Steps: []workflow.StepSpec{validActivity}}
		},
		"unknown mode": func() workflow.DefinitionSpec {
			return workflow.DefinitionSpec{Name: "payments", Version: "1", Mode: workflow.ExecutionMode(99), Steps: []workflow.StepSpec{validActivity}}
		},
		"duplicate step": func() workflow.DefinitionSpec {
			return workflow.DefinitionSpec{Name: "payments", Version: "1", Mode: workflow.Orchestration, Steps: []workflow.StepSpec{validActivity, validActivity}}
		},
		"activity without target": func() workflow.DefinitionSpec {
			step := validActivity
			step.Target = ""
			return workflow.DefinitionSpec{Name: "payments", Version: "1", Mode: workflow.Orchestration, Steps: []workflow.StepSpec{step}}
		},
		"activity without timeout": func() workflow.DefinitionSpec {
			step := validActivity
			step.Timeout = 0
			return workflow.DefinitionSpec{Name: "payments", Version: "1", Mode: workflow.Orchestration, Steps: []workflow.StepSpec{step}}
		},
		"activity without bounded input": func() workflow.DefinitionSpec {
			step := validActivity
			step.InputLimit = 0
			return workflow.DefinitionSpec{Name: "payments", Version: "1", Mode: workflow.Orchestration, Steps: []workflow.StepSpec{step}}
		},
		"activity without bounded result": func() workflow.DefinitionSpec {
			step := validActivity
			step.ResultLimit = 0
			return workflow.DefinitionSpec{Name: "payments", Version: "1", Mode: workflow.Orchestration, Steps: []workflow.StepSpec{step}}
		},
		"activity without bounded retry": func() workflow.DefinitionSpec {
			step := validActivity
			step.Retry.MaxAttempts = 0
			return workflow.DefinitionSpec{Name: "payments", Version: "1", Mode: workflow.Orchestration, Steps: []workflow.StepSpec{step}}
		},
		"unbounded fan out": func() workflow.DefinitionSpec {
			return workflow.DefinitionSpec{Name: "batch", Version: "1", Mode: workflow.Orchestration, Steps: []workflow.StepSpec{{Name: "map", Kind: workflow.StepParallel}}}
		},
		"invalid durable wait": func() workflow.DefinitionSpec {
			return workflow.DefinitionSpec{Name: "payments", Version: "1", Mode: workflow.Orchestration, Steps: []workflow.StepSpec{{Name: "signal", Kind: workflow.StepSignal, Target: "payment.ready", Timeout: 0, InputLimit: 1}}}
		},
		"invalid durable timer": func() workflow.DefinitionSpec {
			return workflow.DefinitionSpec{Name: "payments", Version: "1", Mode: workflow.Orchestration, Steps: []workflow.StepSpec{{Name: "timer", Kind: workflow.StepTimer}}}
		},
		"unknown step kind": func() workflow.DefinitionSpec {
			step := validActivity
			step.Kind = workflow.StepKind(99)
			return workflow.DefinitionSpec{Name: "payments", Version: "1", Mode: workflow.Orchestration, Steps: []workflow.StepSpec{step}}
		},
		"invalid step name": func() workflow.DefinitionSpec {
			step := validActivity
			step.Name = " spaces "
			return workflow.DefinitionSpec{Name: "payments", Version: "1", Mode: workflow.Orchestration, Steps: []workflow.StepSpec{step}}
		},
		"payload exceeds bound": func() workflow.DefinitionSpec {
			step := validActivity
			step.ResultLimit = workflow.MaxPayloadBytes + 1
			return workflow.DefinitionSpec{Name: "payments", Version: "1", Mode: workflow.Orchestration, Steps: []workflow.StepSpec{step}}
		},
		"retry delay is incoherent": func() workflow.DefinitionSpec {
			step := validActivity
			step.Retry.MaxDelay = time.Millisecond
			return workflow.DefinitionSpec{Name: "payments", Version: "1", Mode: workflow.Orchestration, Steps: []workflow.StepSpec{step}}
		},
		"retry initial delay is unbounded": func() workflow.DefinitionSpec {
			step := validActivity
			step.Retry.InitialDelay = 0
			return workflow.DefinitionSpec{Name: "payments", Version: "1", Mode: workflow.Orchestration, Steps: []workflow.StepSpec{step}}
		},
		"compensation only follows activity": func() workflow.DefinitionSpec {
			return workflow.DefinitionSpec{Name: "payments", Version: "1", Mode: workflow.Orchestration, Steps: []workflow.StepSpec{{
				Name: "wait", Kind: workflow.StepTimer, Timeout: time.Second,
				Compensation: &workflow.CompensationSpec{Target: "undo", Timeout: time.Second, ResultLimit: 1, Retry: workflow.RetryPolicy{MaxAttempts: 1, InitialDelay: time.Second, MaxDelay: time.Second}},
			}}}
		},
		"invalid compensation": func() workflow.DefinitionSpec {
			step := validActivity
			step.Compensation = &workflow.CompensationSpec{
				Target: "undo", Timeout: 0, ResultLimit: 1,
				Retry: workflow.RetryPolicy{MaxAttempts: 1, InitialDelay: time.Second, MaxDelay: time.Second},
			}
			return workflow.DefinitionSpec{Name: "payments", Version: "1", Mode: workflow.Orchestration, Steps: []workflow.StepSpec{step}}
		},
		"too many steps": func() workflow.DefinitionSpec {
			steps := make([]workflow.StepSpec, workflow.MaxFanOut+1)
			return workflow.DefinitionSpec{Name: "payments", Version: "1", Mode: workflow.Orchestration, Steps: steps}
		},
	}

	for name, spec := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := workflow.NewDefinition(spec())
			if !errors.Is(err, workflow.ErrInvalidDefinition) {
				t.Fatalf("error = %v, want ErrInvalidDefinition", err)
			}
		})
	}
}

func TestDefinitionRejectsAmbiguousControlFlowBranches(t *testing.T) {
	t.Parallel()

	activity := workflow.StepSpec{
		Name: "work", Kind: workflow.StepActivity, Target: "work.execute",
		Timeout: time.Second, InputLimit: 1, ResultLimit: 1,
		Retry: workflow.RetryPolicy{MaxAttempts: 1, InitialDelay: time.Second, MaxDelay: time.Second},
	}
	controls := []workflow.StepSpec{
		{Name: "missing", Kind: workflow.StepParallel, FanOutLimit: 1, Branches: []string{"absent"}},
		{Name: "malformed", Kind: workflow.StepParallel, FanOutLimit: 1, Branches: []string{" spaces "}},
		{Name: "duplicate", Kind: workflow.StepParallel, FanOutLimit: 2, Branches: []string{"work", "work"}},
	}
	for _, control := range controls {
		if _, err := workflow.NewDefinition(workflow.DefinitionSpec{
			Name: "control", Version: control.Name, Mode: workflow.Orchestration,
			Steps: []workflow.StepSpec{control, activity},
		}); !errors.Is(err, workflow.ErrInvalidDefinition) {
			t.Fatalf("control %q error = %v", control.Name, err)
		}
	}
	activity.Branches = []string{"work"}
	if _, err := workflow.NewDefinition(workflow.DefinitionSpec{
		Name: "control", Version: "activity-branches", Mode: workflow.Orchestration,
		Steps: []workflow.StepSpec{activity},
	}); !errors.Is(err, workflow.ErrInvalidDefinition) {
		t.Fatalf("activity branches error = %v", err)
	}

	activity.Branches = nil
	other := activity
	other.Name = "other"
	third := activity
	third.Name = "third"
	duplicateOwners := []workflow.StepSpec{
		{Name: "first", Kind: workflow.StepParallel, FanOutLimit: 1, Branches: []string{"work"}},
		{Name: "second", Kind: workflow.StepParallel, FanOutLimit: 1, Branches: []string{"work"}},
		activity,
	}
	mismatchedJoin := []workflow.StepSpec{
		{Name: "fan-out", Kind: workflow.StepParallel, FanOutLimit: 1, Branches: []string{"work"}},
		activity,
		other,
		{Name: "join", Kind: workflow.StepJoin, FanOutLimit: 1, Branches: []string{"other"}},
	}
	child := workflow.StepSpec{
		Name: "child", Kind: workflow.StepChild, Target: "child.workflow", Timeout: time.Second,
		ChildDefinition: mustDefinition(t, "child.workflow", "1").Reference(),
		InputLimit:      1, ResultLimit: 1,
		Retry: workflow.RetryPolicy{MaxAttempts: 1, InitialDelay: time.Second, MaxDelay: time.Second},
	}
	nonActivityBranch := []workflow.StepSpec{
		{Name: "fan-out", Kind: workflow.StepParallel, FanOutLimit: 1, Branches: []string{"child"}},
		child,
	}
	nonSignalRace := []workflow.StepSpec{
		{Name: "race", Kind: workflow.StepRace, FanOutLimit: 1, Branches: []string{"work"}},
		activity,
	}
	signal := workflow.StepSpec{
		Name: "signal", Kind: workflow.StepSignal, Target: "race.signal", Timeout: time.Second, InputLimit: 1,
	}
	duplicateRaceOwners := []workflow.StepSpec{
		{Name: "race-one", Kind: workflow.StepRace, FanOutLimit: 1, Branches: []string{"signal"}},
		{Name: "race-two", Kind: workflow.StepRace, FanOutLimit: 1, Branches: []string{"signal"}},
		signal,
	}
	duplicateJoin := []workflow.StepSpec{
		{Name: "fan-out", Kind: workflow.StepParallel, FanOutLimit: 1, Branches: []string{"work"}},
		activity,
		{Name: "join-one", Kind: workflow.StepJoin, FanOutLimit: 1, Branches: []string{"work"}},
		{Name: "join-two", Kind: workflow.StepJoin, FanOutLimit: 1, Branches: []string{"work"}},
	}
	mixedJoin := []workflow.StepSpec{
		{Name: "first", Kind: workflow.StepParallel, FanOutLimit: 2, Branches: []string{"work", "third"}},
		activity,
		third,
		{Name: "second", Kind: workflow.StepParallel, FanOutLimit: 1, Branches: []string{"other"}},
		other,
		{Name: "join", Kind: workflow.StepJoin, FanOutLimit: 2, Branches: []string{"work", "other"}},
	}
	for name, steps := range map[string][]workflow.StepSpec{
		"duplicate-owners": duplicateOwners,
		"duplicate-join":   duplicateJoin,
		"mismatched-join":  mismatchedJoin,
		"mixed-join":       mixedJoin,
		"non-activity":     nonActivityBranch,
		"non-signal-race":  nonSignalRace,
		"duplicate-race":   duplicateRaceOwners,
	} {
		if _, err := workflow.NewDefinition(workflow.DefinitionSpec{
			Name: "control", Version: name, Mode: workflow.Orchestration, Steps: steps,
		}); !errors.Is(err, workflow.ErrInvalidDefinition) {
			t.Fatalf("%s error = %v", name, err)
		}
	}
}

func TestDefinitionRejectsUnpinnedOrMisplacedChildReferences(t *testing.T) {
	t.Parallel()

	child := mustDefinition(t, "child", "1")
	valid := workflow.StepSpec{
		Name: "child", Kind: workflow.StepChild, Target: "child", ChildDefinition: child.Reference(),
		Timeout: time.Second, InputLimit: 1, ResultLimit: 1,
		Retry: workflow.RetryPolicy{MaxAttempts: 1, InitialDelay: time.Second, MaxDelay: time.Second},
	}
	invalid := []workflow.StepSpec{
		func() workflow.StepSpec { value := valid; value.Retry = workflow.RetryPolicy{}; return value }(),
		func() workflow.StepSpec {
			value := valid
			value.ChildDefinition = workflow.DefinitionReference{}
			return value
		}(),
		func() workflow.StepSpec { value := valid; value.Target = "other"; return value }(),
		func() workflow.StepSpec { value := valid; value.Kind = workflow.StepActivity; return value }(),
	}
	for index, step := range invalid {
		if _, err := workflow.NewDefinition(workflow.DefinitionSpec{
			Name: "parent", Version: "child-" + strconv.Itoa(index), Mode: workflow.Orchestration,
			Steps: []workflow.StepSpec{step},
		}); !errors.Is(err, workflow.ErrInvalidDefinition) {
			t.Fatalf("invalid child reference %d error = %v", index, err)
		}
	}
}

func TestRegistryPinsVersionsAndRejectsDuplicateRegistration(t *testing.T) {
	t.Parallel()

	first := mustDefinition(t, "orders", "1")
	second := mustDefinition(t, "orders", "2")

	registry, err := workflow.CompileDefinitions(first, second)
	if err != nil {
		t.Fatalf("compile definitions: %v", err)
	}

	resolved, err := registry.Resolve("orders", "1")
	if err != nil {
		t.Fatalf("resolve pinned version: %v", err)
	}
	if resolved.Fingerprint() != first.Fingerprint() {
		t.Fatal("registry did not return the pinned immutable behavior")
	}
	if _, err := registry.Resolve("orders", "3"); !errors.Is(err, workflow.ErrDefinitionNotFound) {
		t.Fatalf("missing version error = %v", err)
	}
	if _, err := workflow.CompileDefinitions(first, first); !errors.Is(err, workflow.ErrDuplicateDefinition) {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestRegistryRequiresExplicitDefinitionMigration(t *testing.T) {
	t.Parallel()

	first := mustDefinition(t, "orders", "1")
	second := mustDefinition(t, "orders", "2")
	migrate := func(state workflow.MigrationState) (workflow.MigrationState, error) {
		state.Data = append([]byte("v2:"), state.Data...)
		return state, nil
	}

	registry, err := workflow.CompileRegistry(
		[]workflow.Definition{first, second},
		[]workflow.Migration{{Name: "orders", FromVersion: "1", ToVersion: "2", Apply: migrate}},
	)
	if err != nil {
		t.Fatalf("compile registry: %v", err)
	}

	migration, err := registry.Migration("orders", "1", "2")
	if err != nil {
		t.Fatalf("resolve migration: %v", err)
	}
	state, err := migration.Apply(workflow.MigrationState{Data: []byte("state")})
	if err != nil {
		t.Fatalf("apply migration: %v", err)
	}
	if string(state.Data) != "v2:state" {
		t.Fatalf("migration result = %q", state.Data)
	}

	_, err = workflow.CompileRegistry(
		[]workflow.Definition{first, second},
		[]workflow.Migration{{Name: "orders", FromVersion: "1", ToVersion: "3", Apply: migrate}},
	)
	if !errors.Is(err, workflow.ErrInvalidMigration) {
		t.Fatalf("unknown target error = %v", err)
	}
	if _, err := registry.Migration("orders", "2", "1"); !errors.Is(err, workflow.ErrMigrationNotFound) {
		t.Fatalf("missing migration error = %v", err)
	}
	if _, err := (*workflow.Registry)(nil).Migration("orders", "1", "2"); !errors.Is(err, workflow.ErrMigrationNotFound) {
		t.Fatalf("nil registry migration error = %v", err)
	}
}

func TestRegistryRejectsInvalidDefinitionsAndMigrationEdges(t *testing.T) {
	t.Parallel()

	first := mustDefinition(t, "orders", "1")
	second := mustDefinition(t, "orders", "2")
	migrate := func(state workflow.MigrationState) (workflow.MigrationState, error) { return state, nil }

	tests := map[string]struct {
		definitions []workflow.Definition
		migrations  []workflow.Migration
		want        error
	}{
		"zero definition": {
			definitions: []workflow.Definition{{}},
			want:        workflow.ErrInvalidDefinition,
		},
		"nil migration": {
			definitions: []workflow.Definition{first, second},
			migrations:  []workflow.Migration{{Name: "orders", FromVersion: "1", ToVersion: "2"}},
			want:        workflow.ErrInvalidMigration,
		},
		"identity migration": {
			definitions: []workflow.Definition{first},
			migrations:  []workflow.Migration{{Name: "orders", FromVersion: "1", ToVersion: "1", Apply: migrate}},
			want:        workflow.ErrInvalidMigration,
		},
		"missing source": {
			definitions: []workflow.Definition{second},
			migrations:  []workflow.Migration{{Name: "orders", FromVersion: "1", ToVersion: "2", Apply: migrate}},
			want:        workflow.ErrInvalidMigration,
		},
		"duplicate migration": {
			definitions: []workflow.Definition{first, second},
			migrations: []workflow.Migration{
				{Name: "orders", FromVersion: "1", ToVersion: "2", Apply: migrate},
				{Name: "orders", FromVersion: "1", ToVersion: "2", Apply: migrate},
			},
			want: workflow.ErrDuplicateMigration,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := workflow.CompileRegistry(test.definitions, test.migrations)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}

	if _, err := (*workflow.Registry)(nil).Resolve("orders", "1"); !errors.Is(err, workflow.ErrDefinitionNotFound) {
		t.Fatalf("nil registry resolve error = %v", err)
	}
}

func mustDefinition(t *testing.T, name, version string) workflow.Definition {
	t.Helper()

	definition, err := workflow.NewDefinition(workflow.DefinitionSpec{
		Name:    name,
		Version: version,
		Mode:    workflow.Orchestration,
		Steps: []workflow.StepSpec{{
			Name:        "execute",
			Kind:        workflow.StepActivity,
			Target:      name + ".execute",
			Timeout:     time.Minute,
			InputLimit:  1024,
			ResultLimit: 1024,
			Retry: workflow.RetryPolicy{
				MaxAttempts:  1,
				InitialDelay: time.Second,
				MaxDelay:     time.Second,
			},
		}},
	})
	if err != nil {
		t.Fatalf("construct definition: %v", err)
	}

	return definition
}
