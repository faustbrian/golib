package config_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	config "github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/defaults"
	"github.com/faustbrian/golib/pkg/config/dotenv"
	"github.com/faustbrian/golib/pkg/config/environment"
	jsonsource "github.com/faustbrian/golib/pkg/config/json"
	"github.com/faustbrian/golib/pkg/config/programmatic"
)

func TestNewPlanEveryPrioritySubsetAndOrderHasTheDocumentedWinner(t *testing.T) {
	t.Parallel()

	wantOrder := []string{
		"defaults",
		"discovered-base",
		"discovered-profile",
		"explicit-files",
		"dotenv",
		"environment",
		"overrides",
	}
	sources := make([]config.Source, len(wantOrder))
	for index, name := range wantOrder {
		sources[index] = source{
			info: config.SourceInfo{Name: name, Priority: (index + 1) * 10},
			tree: map[string]any{"winner": name},
		}
	}

	combinations := 0
	for mask := 1; mask < 1<<len(sources); mask++ {
		subset := make([]config.Source, 0, len(sources))
		wantSubsetOrder := make([]string, 0, len(sources))
		for index, source := range sources {
			if mask&(1<<index) == 0 {
				continue
			}
			subset = append(subset, source)
			wantSubsetOrder = append(wantSubsetOrder, wantOrder[index])
		}
		forEachSourcePermutation(subset, func(permutation []config.Source) {
			combinations++
			plan, err := config.NewPlan(permutation...)
			if err != nil {
				t.Fatalf("NewPlan() combination %d error = %v", combinations, err)
			}
			gotOrder := make([]string, len(wantSubsetOrder))
			for index, info := range plan.Sources() {
				gotOrder[index] = info.Name
			}
			if !reflect.DeepEqual(gotOrder, wantSubsetOrder) {
				t.Fatalf(
					"NewPlan() combination %d order = %v, want %v",
					combinations,
					gotOrder,
					wantSubsetOrder,
				)
			}
			snapshot, err := config.LoadTree(context.Background(), plan)
			if err != nil {
				t.Fatalf("LoadTree() combination %d error = %v", combinations, err)
			}
			wantWinner := wantSubsetOrder[len(wantSubsetOrder)-1]
			if got := snapshot.Value()["winner"]; got != wantWinner {
				t.Fatalf(
					"LoadTree() combination %d winner = %q, want %q",
					combinations,
					got,
					wantWinner,
				)
			}
		})
	}

	if combinations != 13_699 {
		t.Fatalf("combination count = %d, want 13699", combinations)
	}
}

func TestNewPlanEveryEqualPriorityPermutationPreservesCallerOrder(t *testing.T) {
	t.Parallel()

	sources := make([]config.Source, 4)
	for index := range sources {
		name := fmt.Sprintf("source-%d", index)
		sources[index] = source{
			info: config.SourceInfo{Name: name, Priority: 40},
			tree: map[string]any{"winner": name},
		}
	}

	permutations := 0
	forEachSourcePermutation(sources, func(permutation []config.Source) {
		permutations++
		plan, err := config.NewPlan(permutation...)
		if err != nil {
			t.Fatalf("NewPlan() permutation %d error = %v", permutations, err)
		}
		for index, info := range plan.Sources() {
			if want := permutation[index].Info().Name; info.Name != want {
				t.Fatalf("NewPlan() permutation %d source %d = %q, want %q", permutations, index, info.Name, want)
			}
		}
		snapshot, err := config.LoadTree(context.Background(), plan)
		if err != nil {
			t.Fatalf("LoadTree() permutation %d error = %v", permutations, err)
		}
		wantWinner := permutation[len(permutation)-1].Info().Name
		if got := snapshot.Value()["winner"]; got != wantWinner {
			t.Fatalf("LoadTree() permutation %d winner = %q, want %q", permutations, got, wantWinner)
		}
	})

	if permutations != 24 {
		t.Fatalf("permutation count = %d, want 24", permutations)
	}
}

func TestDefaultCompositionPreservesPresenceAndHighestPrecedenceWinner(t *testing.T) {
	t.Setenv("GO_CONFIG_AUDIT_WINNER", "environment")

	type settings struct {
		TypedDefault config.Optional[string] `config:"typed_default" default:"typed"`
		MapDefault   config.Optional[string] `config:"map_default"`
		Empty        config.Optional[string] `config:"empty"`
		Zero         config.Optional[int]    `config:"zero" env:"ZERO"`
		Null         config.Optional[string] `config:"null"`
		Absent       config.Optional[string] `config:"absent"`
		Winner       config.Optional[string] `config:"winner" env:"WINNER"`
	}

	typedDefaults, err := defaults.For[settings]("typed-defaults")
	if err != nil {
		t.Fatalf("defaults.For() error = %v", err)
	}
	mapDefaults, err := programmatic.Defaults(
		"map-defaults",
		map[string]any{"map_default": "mapped", "winner": "defaults"},
	)
	if err != nil {
		t.Fatalf("programmatic.Defaults() error = %v", err)
	}
	explicitFile, err := jsonsource.Bytes(
		[]byte(`{"empty":"","null":null,"winner":"file"}`),
		jsonsource.Options{Name: "explicit-file"},
	)
	if err != nil {
		t.Fatalf("json.Bytes() error = %v", err)
	}
	dotenvSource, err := dotenv.BytesFor[settings](
		[]byte("GO_CONFIG_AUDIT_ZERO=0\nGO_CONFIG_AUDIT_WINNER=dotenv\n"),
		dotenv.Options{Name: "dotenv", Prefix: "GO_CONFIG_AUDIT_"},
	)
	if err != nil {
		t.Fatalf("dotenv.BytesFor() error = %v", err)
	}
	environmentSource, err := environment.ProcessFor[settings](environment.Options{
		Name: "environment", Prefix: "GO_CONFIG_AUDIT_",
	})
	if err != nil {
		t.Fatalf("environment.ProcessFor() error = %v", err)
	}
	overrides, err := programmatic.Overrides(
		"overrides",
		map[string]any{"winner": "overrides"},
	)
	if err != nil {
		t.Fatalf("programmatic.Overrides() error = %v", err)
	}

	plan, err := config.NewDefaultPlan(config.DefaultSources{
		Defaults:      []config.Source{typedDefaults, mapDefaults},
		ExplicitFiles: []config.Source{explicitFile},
		Dotenv:        []config.Source{dotenvSource},
		Environment:   []config.Source{environmentSource},
		Overrides:     []config.Source{overrides},
	})
	if err != nil {
		t.Fatalf("NewDefaultPlan() error = %v", err)
	}
	snapshot, err := config.Load[settings](context.Background(), plan)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got := snapshot.Value()

	assertOptionalString(t, "typed default", got.TypedDefault, config.Defaulted, "typed", true)
	assertOptionalString(t, "map default", got.MapDefault, config.Defaulted, "mapped", true)
	assertOptionalString(t, "empty", got.Empty, config.Present, "", true)
	if value, ok := got.Zero.Get(); got.Zero.State() != config.Present || !ok || value != 0 {
		t.Fatalf("zero = state %v, value %d, ok %v", got.Zero.State(), value, ok)
	}
	assertOptionalString(t, "null", got.Null, config.Null, "", false)
	assertOptionalString(t, "absent", got.Absent, config.Absent, "", false)
	assertOptionalString(t, "winner", got.Winner, config.Present, "overrides", true)
}

func forEachSourcePermutation(sources []config.Source, visit func([]config.Source)) {
	values := append([]config.Source(nil), sources...)
	var permute func(int)
	permute = func(index int) {
		if index == len(values) {
			visit(values)
			return
		}
		for candidate := index; candidate < len(values); candidate++ {
			values[index], values[candidate] = values[candidate], values[index]
			permute(index + 1)
			values[index], values[candidate] = values[candidate], values[index]
		}
	}
	permute(0)
}

func assertOptionalString(
	t *testing.T,
	name string,
	value config.Optional[string],
	wantState config.Presence,
	wantValue string,
	wantOK bool,
) {
	t.Helper()
	gotValue, gotOK := value.Get()
	if value.State() != wantState || gotValue != wantValue || gotOK != wantOK {
		t.Fatalf(
			"%s = state %v, value %q, ok %v; want state %v, value %q, ok %v",
			name,
			value.State(),
			gotValue,
			gotOK,
			wantState,
			wantValue,
			wantOK,
		)
	}
}
