package config

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestLoadWithValidatorsPropagatesTreeLoadFailureInternally(t *testing.T) {
	t.Parallel()

	failure := errors.New("load failure")
	plan, err := NewPlan(internalHardeningSource{
		info: SourceInfo{Name: "failure"}, err: failure,
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	if _, err := LoadWithValidators[struct{}](context.Background(), plan); !errors.Is(err, failure) {
		t.Fatalf("LoadWithValidators() error = %v, want load failure", err)
	}
}

func TestPresenceUsesDefaultAndCommaSuffixedFieldNamesInternally(t *testing.T) {
	t.Parallel()

	type configuration struct {
		Default Optional[int]
		Tagged  Optional[int] `config:"tagged,omitempty"`
	}
	plan, err := NewPlan(internalHardeningSource{
		info: SourceInfo{Name: "values"},
		document: Document{Tree: map[string]any{
			"default": int64(1), "tagged": int64(2),
		}},
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	snapshot, err := Load[configuration](context.Background(), plan)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got := snapshot.Value()
	if got.Default.State() != Present || got.Tagged.State() != Present {
		t.Fatalf("presence states = %v, %v", got.Default.State(), got.Tagged.State())
	}
}

func TestOptionalReportsItsTypedTextTargetInternally(t *testing.T) {
	t.Parallel()

	if got := (Optional[int]{}).ConfigTextTarget(); got != reflect.TypeFor[int]() {
		t.Fatalf("ConfigTextTarget() = %v", got)
	}
}

func TestCloneReflectReconstructsPointerFromValueCloneInternally(t *testing.T) {
	t.Parallel()

	original := &internalValueReturningCloner{Value: 42}
	cloned := cloneTyped(original)
	if cloned == original || cloned.Value != 42 {
		t.Fatalf("cloneTyped() = %#v", cloned)
	}
	cloned.Value = 7
	if original.Value != 42 {
		t.Fatalf("original mutated to %d", original.Value)
	}
}

func TestCanonicalTreePropagatesNestedArrayValueErrorsInternally(t *testing.T) {
	t.Parallel()

	value := 42
	canonicalizer := treeCanonicalizer{
		ctx: context.Background(), visiting: make(map[treeVisit]bool),
	}
	_, err := canonicalizer.value([]any{"safe", &value}, "items", 1)
	var valueErr *TreeValueError
	if !errors.As(err, &valueErr) || valueErr.Path != "items[1]" {
		t.Fatalf("canonicalTreeValue() error = %#v", err)
	}
}

func TestCanonicalTreeChecksContextAtObjectBoundariesInternally(t *testing.T) {
	t.Parallel()

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	canonicalizer := treeCanonicalizer{
		ctx: canceled, visiting: make(map[treeVisit]bool),
	}
	if _, err := canonicalizer.object(map[string]any{}, "", 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("object(canceled) error = %v", err)
	}

	canonicalizer = treeCanonicalizer{
		ctx:      &internalCanonicalContext{cancelAfter: 1},
		visiting: make(map[treeVisit]bool),
	}
	if _, err := canonicalizer.object(map[string]any{"value": true}, "", 1); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("object(cancel during keys) error = %v", err)
	}
}

func TestSnapshotCloneGuardsCoverNestedMutableShapesInternally(t *testing.T) {
	t.Parallel()

	if err := validateSnapshotValue(reflect.Value{}, "", nil); err != nil {
		t.Fatalf("validateSnapshotValue(invalid) error = %v", err)
	}
	if err := validateSnapshotValue(
		reflect.ValueOf(time.Unix(1, 0)),
		"started",
		make(map[cloneVisit]bool),
	); err != nil {
		t.Fatalf("validateSnapshotValue(time.Time) error = %v", err)
	}

	type node struct{ Next *node }
	cycle := &node{}
	cycle.Next = cycle
	assertSnapshotValueError(t, reflect.ValueOf(cycle), "cycle")

	assertSnapshotValueError(t, reflect.ValueOf(map[chan int]string{
		make(chan int): "value",
	}), "map-key")
	assertSnapshotValueError(t, reflect.ValueOf(map[string]any{
		"value": func() {},
	}), "map-value")
	assertSnapshotValueError(t, reflect.ValueOf([]any{func() {}}), "slice")
	assertSnapshotValueError(t, reflect.ValueOf(func() {}), "function")

	type nestedMutable struct {
		Safe   string
		Unsafe map[string]string
	}
	if !typeContainsMutableReferences(reflect.TypeFor[[1]*int]()) ||
		typeContainsMutableReferences(reflect.TypeFor[[1]int]()) ||
		!typeContainsMutableReferences(reflect.TypeFor[nestedMutable]()) {
		t.Fatal("typeContainsMutableReferences() classified a nested shape incorrectly")
	}

	err := snapshotValueError("field", reflect.TypeFor[map[string]string]())
	if err.Error() !=
		`config snapshot at "field": value type map[string]string is not safely cloneable` {
		t.Fatalf("SnapshotValueError.Error() = %q", err)
	}
}

func assertSnapshotValueError(t *testing.T, value reflect.Value, path string) {
	t.Helper()
	err := validateSnapshotValue(value, path, make(map[cloneVisit]bool))
	var valueErr *SnapshotValueError
	if !errors.As(err, &valueErr) {
		t.Fatalf("validateSnapshotValue(%s) error = %T %v", path, err, err)
	}
}

type internalHardeningSource struct {
	info     SourceInfo
	document Document
	err      error
}

func (s internalHardeningSource) Info() SourceInfo { return s.info }
func (s internalHardeningSource) Load(context.Context) (Document, error) {
	return s.document, s.err
}

type internalValueReturningCloner struct{ Value int }

func (c *internalValueReturningCloner) cloneConfigValue() any { return *c }

type internalCanonicalContext struct {
	calls       int
	cancelAfter int
}

func (*internalCanonicalContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*internalCanonicalContext) Done() <-chan struct{}       { return nil }
func (*internalCanonicalContext) Value(any) any               { return nil }
func (c *internalCanonicalContext) Err() error {
	c.calls++
	if c.calls > c.cancelAfter {
		return context.Canceled
	}
	return nil
}
