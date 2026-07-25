package config_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	config "github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/decode"
	"github.com/faustbrian/golib/pkg/config/merge"
)

func TestLoadTreeOnlySuppressesAbsentOptionalSources(t *testing.T) {
	t.Parallel()

	absent := source{
		info: config.SourceInfo{Name: "optional", Priority: 10, Optional: true},
		err:  errors.Join(errors.New("missing fixture"), config.ErrNotFound),
	}
	present := source{
		info: config.SourceInfo{Name: "present", Priority: 20},
		tree: map[string]any{"value": "loaded"},
	}
	plan, err := config.NewPlan(absent, present)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	snapshot, err := config.LoadTree(context.Background(), plan)
	if err != nil || snapshot.Value()["value"] != "loaded" {
		t.Fatalf("LoadTree() = %#v, %v", snapshot, err)
	}

	malformed := errors.New("malformed optional source")
	plan, err = config.NewPlan(source{
		info: config.SourceInfo{Name: "optional", Optional: true}, err: malformed,
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	snapshot, err = config.LoadTree(context.Background(), plan)
	if snapshot != nil || !errors.Is(err, malformed) {
		t.Fatalf("LoadTree() = %#v, %v; want malformed error", snapshot, err)
	}

	plan, err = config.NewPlan(source{
		info: config.SourceInfo{Name: "required"}, err: config.ErrNotFound,
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	if _, err := config.LoadTree(context.Background(), plan); !errors.Is(err, config.ErrNotFound) {
		t.Fatalf("LoadTree() error = %v, want ErrNotFound", err)
	}
}

func TestLoadTreeRedactsArbitrarySourceErrors(t *testing.T) {
	t.Parallel()

	const canary = "canary-secret-source-value"
	cause := errors.New(canary)
	plan, err := config.NewPlan(source{
		info: config.SourceInfo{Name: "secret-source", Sensitive: true},
		err:  cause,
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	snapshot, err := config.LoadTree(context.Background(), plan)
	var sourceErr *config.SourceError
	if snapshot != nil || !errors.As(err, &sourceErr) || !errors.Is(err, cause) {
		t.Fatalf("LoadTree() = %#v, %T %v", snapshot, err, err)
	}
	for _, formatted := range []string{
		err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err),
		fmt.Sprintf("%#v", err),
	} {
		if strings.Contains(formatted, canary) {
			t.Fatalf("LoadTree() error leaked source cause: %q", formatted)
		}
	}
}

func TestLoadTreeIsCanceledBeforeLoadingOrMerging(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plan, err := config.NewPlan(source{
		info: config.SourceInfo{Name: "never"}, tree: map[string]any{"value": true},
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	snapshot, err := config.LoadTree(ctx, plan)
	if snapshot != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("LoadTree() = %#v, %v; want context.Canceled", snapshot, err)
	}

	plan, err = config.NewPlan(
		source{info: config.SourceInfo{Name: "lower"}, tree: map[string]any{"value": "text"}},
		source{info: config.SourceInfo{Name: "upper", Priority: 1}, tree: map[string]any{"value": []any{}}},
	)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	snapshot, err = config.LoadTree(context.Background(), plan)
	var conflict *merge.TypeConflictError
	if snapshot != nil || !errors.As(err, &conflict) || conflict.Path != "value" {
		t.Fatalf("LoadTree() = %#v, %v; want value conflict", snapshot, err)
	}
}

func TestLoadTreeTracksNullDeletionAndOriginOverrides(t *testing.T) {
	t.Parallel()

	plan, err := config.NewPlan(
		source{
			info: config.SourceInfo{Name: "lower", Priority: 10},
			tree: map[string]any{
				"nullable": map[string]any{"leaf": "value"},
				"deleted":  map[string]any{"leaf": "value"},
				"custom":   "lower",
			},
		},
		source{
			info: config.SourceInfo{Name: "upper", Priority: 20},
			tree: map[string]any{
				"nullable": nil,
				"deleted":  merge.Delete{},
				"custom":   "upper",
			},
			origins: map[string]config.Origin{
				"custom": {
					Source: "parser", Location: "settings.yaml:4", Sensitive: true,
					Present: false, State: config.Defaulted,
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	snapshot, err := config.LoadTree(context.Background(), plan)
	if err != nil {
		t.Fatalf("LoadTree() error = %v", err)
	}
	wantTree := map[string]any{"nullable": nil, "custom": "upper"}
	if got := snapshot.Value(); !reflect.DeepEqual(got, wantTree) {
		t.Fatalf("Snapshot.Value() = %#v, want %#v", got, wantTree)
	}
	if origin, ok := snapshot.Origin("nullable"); !ok || origin.Source != "upper" || origin.State != config.Null {
		t.Fatalf("nullable origin = %#v, %v", origin, ok)
	}
	for _, path := range []string{"nullable.leaf", "deleted", "deleted.leaf"} {
		if origin, ok := snapshot.Origin(path); ok {
			t.Fatalf("deleted origin %q = %#v", path, origin)
		}
	}
	wantOrigin := config.Origin{
		Source: "parser", Location: "settings.yaml:4", Sensitive: true,
		Present: false, State: config.Defaulted,
	}
	if origin, ok := snapshot.Origin("custom"); !ok || origin != wantOrigin {
		t.Fatalf("custom origin = %#v, %v; want %#v", origin, ok, wantOrigin)
	}
}

func TestLoadAnnotatesEveryDecodeFailureWithNearestOrigin(t *testing.T) {
	t.Parallel()

	type settings struct {
		Count int   `config:"count"`
		Items []int `config:"items"`
	}
	plan, err := config.NewPlan(source{
		info: config.SourceInfo{Name: "settings", Priority: 40},
		tree: map[string]any{"count": "bad", "items": []any{"bad"}},
		origins: map[string]config.Origin{
			"count": {Location: "settings.json:2", Present: true, State: config.Present},
			"items": {Location: "settings.json:3", Present: true, State: config.Present},
		},
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	_, err = config.Load[settings](context.Background(), plan)
	var failures *decode.Errors
	if !errors.As(err, &failures) || len(failures.Fields) != 2 {
		t.Fatalf("Load() error = %T %#v", err, err)
	}
	wantLocations := map[string]string{
		"count": "settings.json:2", "items[0]": "settings.json:3",
	}
	for _, failure := range failures.Fields {
		if failure.Source != "settings" || failure.Location != wantLocations[failure.Path] {
			t.Fatalf("failure = %#v", failure)
		}
	}
}

func TestLoadRequiredFieldWithoutOriginRemainsSafelyUnattributed(t *testing.T) {
	t.Parallel()

	type settings struct {
		Required string `config:"required,required"`
	}
	plan, err := config.NewPlan(source{
		info: config.SourceInfo{Name: "empty"}, tree: map[string]any{},
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	_, err = config.Load[settings](context.Background(), plan)
	var failure *decode.FieldError
	if !errors.As(err, &failure) {
		t.Fatalf("Load() error = %T %v", err, err)
	}
	if failure.Source != "" || failure.Location != "" || strings.Contains(err.Error(), "empty") {
		t.Fatalf("failure unexpectedly attributed: %#v, %q", failure, err)
	}
}

func TestLoadUsesParentOriginForMissingNestedField(t *testing.T) {
	t.Parallel()

	type settings struct {
		Nested struct {
			Required string `config:"required,required"`
		} `config:"nested"`
	}
	plan, err := config.NewPlan(source{
		info: config.SourceInfo{Name: "settings"},
		tree: map[string]any{"nested": map[string]any{}},
		origins: map[string]config.Origin{
			"nested": {Location: "settings.yaml:8", Present: true, State: config.Present},
		},
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	_, err = config.Load[settings](context.Background(), plan)
	var failure *decode.FieldError
	if !errors.As(err, &failure) {
		t.Fatalf("Load() error = %T %v", err, err)
	}
	if failure.Path != "nested.required" || failure.Source != "settings" ||
		failure.Location != "settings.yaml:8" {
		t.Fatalf("failure = %#v", failure)
	}
}

func TestSnapshotValueClonesAllPublicMutableShapes(t *testing.T) {
	t.Parallel()

	type nested struct {
		Values []string `config:"values"`
	}
	type settings struct {
		Pointer    *nested           `config:"pointer"`
		NilPointer *int              `config:"nil_pointer"`
		NilMap     map[string]string `config:"nil_map"`
		NilSlice   []string          `config:"nil_slice"`
		Dynamic    any               `config:"dynamic"`
	}
	plan, err := config.NewPlan(source{
		info: config.SourceInfo{Name: "fixture"},
		tree: map[string]any{
			"pointer": map[string]any{"values": []any{"original"}},
			"dynamic": map[string]any{"items": []any{"original"}},
		},
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	snapshot, err := config.Load[settings](context.Background(), plan)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	first := snapshot.Value()
	if first.NilPointer != nil || first.NilMap != nil || first.NilSlice != nil {
		t.Fatalf("nil fields changed: %#v", first)
	}
	first.Pointer.Values[0] = "mutated"
	first.Dynamic.(map[string]any)["items"].([]any)[0] = "mutated"
	second := snapshot.Value()
	if second.Pointer.Values[0] != "original" ||
		second.Dynamic.(map[string]any)["items"].([]any)[0] != "original" {
		t.Fatalf("Snapshot.Value() exposed mutation: %#v", second)
	}

	var zero config.Snapshot[any]
	if got := zero.Value(); got != nil {
		t.Fatalf("zero Snapshot.Value() = %#v, want nil", got)
	}
}

func TestLoadTreeRejectsMutableNonCanonicalSourceValues(t *testing.T) {
	t.Parallel()

	value := 42
	plan, err := config.NewPlan(source{
		info: config.SourceInfo{Name: "unsafe"},
		tree: map[string]any{"nested": map[string]any{"pointer": &value}},
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	snapshot, err := config.LoadTree(context.Background(), plan)
	var valueErr *config.TreeValueError
	if snapshot != nil || !errors.As(err, &valueErr) {
		t.Fatalf("LoadTree() = %#v, %T %v", snapshot, err, err)
	}
	if valueErr.Path != "nested.pointer" || valueErr.Type != "*int" {
		t.Fatalf("TreeValueError = %#v", valueErr)
	}
}

func TestLoadTreeRejectsCyclicAndBoundedSourceTrees(t *testing.T) {
	t.Parallel()

	cycle := map[string]any{}
	cycle["self"] = cycle
	plan, err := config.NewPlan(source{
		info: config.SourceInfo{Name: "cycle"}, tree: cycle,
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	snapshot, err := config.LoadTree(context.Background(), plan)
	var cycleErr *config.TreeCycleError
	if snapshot != nil || !errors.As(err, &cycleErr) || cycleErr.Path != "self" {
		t.Fatalf("LoadTree(cycle) = %#v, %T %v", snapshot, err, err)
	}
	if cycleErr.Error() != `config tree at "self": cyclic value` {
		t.Fatalf("TreeCycleError.Error() = %q", cycleErr)
	}

	cycleSlice := make([]any, 1)
	cycleSlice[0] = cycleSlice
	plan, _ = config.NewPlan(source{
		info: config.SourceInfo{Name: "slice-cycle"},
		tree: map[string]any{"items": cycleSlice},
	})
	snapshot, err = config.LoadTree(context.Background(), plan)
	if snapshot != nil || !errors.As(err, &cycleErr) || cycleErr.Path != "items[0]" {
		t.Fatalf("LoadTree(slice cycle) = %#v, %T %v", snapshot, err, err)
	}

	deep := map[string]any{}
	cursor := deep
	for range 65 {
		next := map[string]any{}
		cursor["next"] = next
		cursor = next
	}
	plan, _ = config.NewPlan(source{
		info: config.SourceInfo{Name: "deep"}, tree: deep,
	})
	snapshot, err = config.LoadTree(context.Background(), plan)
	var limitErr *config.TreeLimitError
	if snapshot != nil || !errors.As(err, &limitErr) ||
		limitErr.Kind != "depth" || limitErr.Limit != 64 {
		t.Fatalf("LoadTree(deep) = %#v, %T %v", snapshot, err, err)
	}
	if !strings.Contains(limitErr.Error(), "depth exceeds 64 limit") {
		t.Fatalf("TreeLimitError.Error() = %q", limitErr)
	}
}

func TestLoadTreeBoundsKeysAndHonorsCancellationDuringCanonicalization(t *testing.T) {
	t.Parallel()

	wide := make(map[string]any, 100_001)
	for index := range 100_001 {
		wide[fmt.Sprintf("key_%06d", index)] = index
	}
	plan, _ := config.NewPlan(source{
		info: config.SourceInfo{Name: "wide"}, tree: wide,
	})
	snapshot, err := config.LoadTree(context.Background(), plan)
	var limitErr *config.TreeLimitError
	if snapshot != nil || !errors.As(err, &limitErr) ||
		limitErr.Kind != "keys" || limitErr.Limit != 100_000 {
		t.Fatalf("LoadTree(wide) = %#v, %T %v", snapshot, err, err)
	}

	wideItems := make([]any, 100_001)
	plan, _ = config.NewPlan(source{
		info: config.SourceInfo{Name: "wide-items"},
		tree: map[string]any{"items": wideItems},
	})
	snapshot, err = config.LoadTree(context.Background(), plan)
	if snapshot != nil || !errors.As(err, &limitErr) ||
		limitErr.Kind != "items" || limitErr.Limit != 100_000 {
		t.Fatalf("LoadTree(wide items) snapshot nil = %t, error = %T %v", snapshot == nil, err, err)
	}

	plan, _ = config.NewPlan(source{
		info: config.SourceInfo{Name: "cancel"},
		tree: map[string]any{"nested": map[string]any{"value": true}},
	})
	snapshot, err = config.LoadTree(&canonicalCancelContext{cancelAfter: 3}, plan)
	if snapshot != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("LoadTree(cancel) = %#v, %v", snapshot, err)
	}
}

func TestLoadRejectsPrivateMutableStateWithoutSnapshotCloner(t *testing.T) {
	t.Parallel()

	type settings struct {
		Unsafe privateMutableScalar `config:"unsafe"`
	}
	plan, err := config.NewPlan(source{
		info: config.SourceInfo{Name: "fixture"},
		tree: map[string]any{"unsafe": "loaded"},
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	snapshot, err := config.Load[settings](context.Background(), plan)
	var valueErr *config.SnapshotValueError
	if snapshot != nil || !errors.As(err, &valueErr) {
		t.Fatalf("Load() = %#v, %T %v", snapshot, err, err)
	}
	if valueErr.Path != "unsafe.values" || valueErr.Type != "map[string]string" {
		t.Fatalf("SnapshotValueError = %#v", valueErr)
	}
}

type privateMutableScalar struct {
	values map[string]string
}

func (s *privateMutableScalar) UnmarshalText(text []byte) error {
	s.values = map[string]string{"value": string(text)}
	return nil
}

type canonicalCancelContext struct {
	calls       int
	cancelAfter int
}

func (*canonicalCancelContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*canonicalCancelContext) Done() <-chan struct{}       { return nil }
func (*canonicalCancelContext) Value(any) any               { return nil }
func (c *canonicalCancelContext) Err() error {
	c.calls++
	if c.calls > c.cancelAfter {
		return context.Canceled
	}
	return nil
}
