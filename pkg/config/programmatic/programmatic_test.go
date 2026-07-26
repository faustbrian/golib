package programmatic_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	config "github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/programmatic"
)

func TestDefaultsAndOverridesUseDocumentedPriorities(t *testing.T) {
	t.Parallel()

	defaults, err := programmatic.Defaults("defaults", map[string]any{"name": "default"})
	if err != nil {
		t.Fatalf("Defaults() error = %v", err)
	}
	overrides, err := programmatic.Overrides("overrides", map[string]any{"name": "override"})
	if err != nil {
		t.Fatalf("Overrides() error = %v", err)
	}
	if defaults.Info().Priority != config.PriorityDefaults {
		t.Fatalf("Defaults priority = %d", defaults.Info().Priority)
	}
	if overrides.Info().Priority != config.PriorityOverrides {
		t.Fatalf("Overrides priority = %d", overrides.Info().Priority)
	}

	plan, err := config.NewPlan(overrides, defaults)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	snapshot, err := config.LoadTree(context.Background(), plan)
	if err != nil {
		t.Fatalf("LoadTree() error = %v", err)
	}
	if got := snapshot.Value()["name"]; got != "override" {
		t.Fatalf("Snapshot value = %#v, want override", got)
	}
	if origin, ok := snapshot.Origin("name"); !ok || origin.Source != "overrides" {
		t.Fatalf("Snapshot origin = %#v, %v", origin, ok)
	}
}

func TestMapSourceDoesNotMutateOrExposeInput(t *testing.T) {
	t.Parallel()

	input := map[string]any{"nested": map[string]any{"items": []any{"original"}}}
	source, err := programmatic.Map("map", 42, input)
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}
	input["nested"].(map[string]any)["items"].([]any)[0] = "input changed"

	first, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Source.Load() error = %v", err)
	}
	first.Tree["nested"].(map[string]any)["items"].([]any)[0] = "result changed"
	second, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Source.Load() error = %v", err)
	}
	want := map[string]any{"nested": map[string]any{"items": []any{"original"}}}
	if !reflect.DeepEqual(second.Tree, want) {
		t.Fatalf("Source.Load() tree = %#v, want %#v", second.Tree, want)
	}
}

func TestMapRejectsMutablePointerValues(t *testing.T) {
	t.Parallel()

	value := "unsafe"
	source, err := programmatic.Map("map", 42, map[string]any{
		"nested": map[string]any{"pointer": &value},
	})
	if source != nil || err == nil {
		t.Fatalf("Map() = %#v, %v", source, err)
	}
	if !strings.Contains(err.Error(), `"nested.pointer"`) ||
		!strings.Contains(err.Error(), "*string") {
		t.Fatalf("Map() error = %q", err)
	}
}

func TestMapSourceRejectsInvalidNameAndHonorsCancellation(t *testing.T) {
	t.Parallel()

	if _, err := programmatic.Map("", 1, nil); err == nil {
		t.Fatal("Map() error = nil, want invalid name error")
	}
	source, err := programmatic.Map("map", 1, nil)
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := source.Load(ctx); err == nil {
		t.Fatal("Source.Load() error = nil, want cancellation")
	}
}

func TestMapSourceNormalizesTypedCollectionsWithoutAliasing(t *testing.T) {
	t.Parallel()

	type settings struct {
		Hosts  []string          `config:"hosts"`
		Labels map[string]string `config:"labels"`
	}
	hosts := []string{"one", "two"}
	labels := map[string]string{"region": "eu"}
	source, err := programmatic.Map("map", 42, map[string]any{
		"hosts": hosts, "labels": labels,
	})
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}
	hosts[0] = "mutated"
	labels["region"] = "mutated"
	plan, err := config.NewPlan(source)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	snapshot, err := config.Load[settings](context.Background(), plan)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got := snapshot.Value()
	if !reflect.DeepEqual(got.Hosts, []string{"one", "two"}) || got.Labels["region"] != "eu" {
		t.Fatalf("Load() value = %#v", got)
	}
}

func TestMapSourceRejectsNonStringMapKeys(t *testing.T) {
	t.Parallel()

	_, err := programmatic.Map("map", 42, map[string]any{
		"invalid": map[int]string{1: "one"},
	})
	if err == nil {
		t.Fatal("Map() error = nil, want unsupported key error")
	}
}

func TestDefaultsMarkEveryNestedPathAsDefaulted(t *testing.T) {
	t.Parallel()

	source, err := programmatic.Defaults("defaults", map[string]any{
		"server": map[string]any{"host": "localhost"},
	})
	if err != nil {
		t.Fatalf("Defaults() error = %v", err)
	}
	document, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	for _, path := range []string{"server", "server.host"} {
		if origin, ok := document.Origins[path]; !ok || origin.State != config.Defaulted {
			t.Fatalf("origin %q = %#v, %v", path, origin, ok)
		}
	}
}

func TestMapSourceNormalizesArraysNamedKeysAndTypedNilCollections(t *testing.T) {
	t.Parallel()

	type key string
	var nilMap map[string]string
	var nilSlice []string
	source, err := programmatic.Map("map", 42, map[string]any{
		"array":     [2]string{"one", "two"},
		"named_map": map[key]int{"count": 2},
		"nil_map":   nilMap,
		"nil_slice": nilSlice,
		"null":      nil,
	})
	if err != nil {
		t.Fatalf("Map() error = %v", err)
	}
	document, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(document.Tree["array"], []any{"one", "two"}) ||
		!reflect.DeepEqual(document.Tree["named_map"], map[string]any{"count": 2}) ||
		document.Tree["nil_map"] != nil || document.Tree["nil_slice"] != nil {
		t.Fatalf("Load() tree = %#v", document.Tree)
	}
}

func TestMapSourceReportsNestedInvalidMapPath(t *testing.T) {
	t.Parallel()

	for name, input := range map[string]map[string]any{
		"slice": {"items": []any{map[int]string{1: "one"}}},
		"map":   {"object": map[string]any{"invalid": map[int]string{1: "one"}}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := programmatic.Map("map", 42, input)
			if err == nil || !strings.Contains(err.Error(), "invalid") &&
				!strings.Contains(err.Error(), `"items[0]"`) {
				t.Fatalf("Map() error = %v, want nested safe path", err)
			}
		})
	}
}
