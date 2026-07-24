package config_test

import (
	"context"
	"reflect"
	"testing"

	config "github.com/faustbrian/golib/pkg/config"
)

func TestNewDefaultPlanAssignsDocumentedPrecedence(t *testing.T) {
	t.Parallel()

	makeSource := func(name, value string) source {
		return source{
			info: config.SourceInfo{Name: name, Priority: -100},
			tree: map[string]any{"winner": value},
		}
	}
	plan, err := config.NewDefaultPlan(config.DefaultSources{
		Overrides:         []config.Source{makeSource("overrides", "overrides")},
		Environment:       []config.Source{makeSource("environment", "environment")},
		Dotenv:            []config.Source{makeSource("dotenv", "dotenv")},
		ExplicitFiles:     []config.Source{makeSource("explicit-one", "explicit-one"), makeSource("explicit-two", "explicit-two")},
		DiscoveredProfile: []config.Source{makeSource("profile", "profile")},
		DiscoveredBase:    []config.Source{makeSource("base", "base")},
		Defaults:          []config.Source{makeSource("defaults", "defaults")},
	})
	if err != nil {
		t.Fatalf("NewDefaultPlan() error = %v", err)
	}

	want := []config.SourceInfo{
		{Name: "defaults", Priority: config.PriorityDefaults},
		{Name: "base", Priority: config.PriorityDiscoveredBase},
		{Name: "profile", Priority: config.PriorityDiscoveredProfile},
		{Name: "explicit-one", Priority: config.PriorityExplicitFiles},
		{Name: "explicit-two", Priority: config.PriorityExplicitFiles},
		{Name: "dotenv", Priority: config.PriorityDotenv},
		{Name: "environment", Priority: config.PriorityEnvironment},
		{Name: "overrides", Priority: config.PriorityOverrides},
	}
	if got := plan.Sources(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Plan.Sources() = %#v, want %#v", got, want)
	}

	snapshot, err := config.LoadTree(context.Background(), plan)
	if err != nil {
		t.Fatalf("LoadTree() error = %v", err)
	}
	if got := snapshot.Value()["winner"]; got != "overrides" {
		t.Fatalf("LoadTree() winner = %q, want overrides", got)
	}
}

func TestNewDefaultPlanDoesNotMutateSourceMetadata(t *testing.T) {
	t.Parallel()

	original := source{info: config.SourceInfo{Name: "source", Priority: 999}}
	plan, err := config.NewDefaultPlan(config.DefaultSources{Defaults: []config.Source{original}})
	if err != nil {
		t.Fatalf("NewDefaultPlan() error = %v", err)
	}
	if got := original.Info().Priority; got != 999 {
		t.Fatalf("original source priority = %d, want 999", got)
	}
	if got := plan.Sources()[0].Priority; got != config.PriorityDefaults {
		t.Fatalf("plan source priority = %d, want defaults", got)
	}
}

func TestNewDefaultPlanRejectsDuplicateNamesAcrossCategories(t *testing.T) {
	t.Parallel()

	duplicate := source{info: config.SourceInfo{Name: "same"}}
	_, err := config.NewDefaultPlan(config.DefaultSources{
		Defaults: []config.Source{duplicate}, Overrides: []config.Source{duplicate},
	})
	if err == nil {
		t.Fatal("NewDefaultPlan() error = nil, want duplicate source error")
	}
}

func TestNewDefaultPlanRejectsTypedNilSourceWithoutPanicking(t *testing.T) {
	t.Parallel()

	var source *typedNilSource
	if _, err := config.NewDefaultPlan(config.DefaultSources{
		Defaults: []config.Source{source},
	}); err == nil {
		t.Fatal("NewDefaultPlan() error = nil, want typed nil error")
	}
}
