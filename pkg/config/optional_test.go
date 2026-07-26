package config_test

import (
	"context"
	"errors"
	"testing"

	config "github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/decode"
	"github.com/faustbrian/golib/pkg/config/programmatic"
	"github.com/faustbrian/golib/pkg/config/validation"
)

func TestOptionalPreservesAbsentNullEmptyAndZero(t *testing.T) {
	t.Parallel()

	type configuration struct {
		Absent config.Optional[string] `config:"absent"`
		Null   config.Optional[string] `config:"null"`
		Empty  config.Optional[string] `config:"empty"`
		Zero   config.Optional[int]    `config:"zero"`
	}
	var got configuration
	err := decode.Into(map[string]any{
		"null":  nil,
		"empty": "",
		"zero":  int64(0),
	}, &got)
	if err != nil {
		t.Fatalf("Into() error = %v", err)
	}
	if got.Absent.State() != config.Absent {
		t.Fatalf("Absent state = %v", got.Absent.State())
	}
	if got.Null.State() != config.Null {
		t.Fatalf("Null state = %v", got.Null.State())
	}
	if value, ok := got.Empty.Get(); !ok || value != "" {
		t.Fatalf("Empty Get() = %q, %v", value, ok)
	}
	if value, ok := got.Zero.Get(); !ok || value != 0 {
		t.Fatalf("Zero Get() = %d, %v", value, ok)
	}
}

func TestOptionalCollectionsRemainImmutableAcrossSnapshotReads(t *testing.T) {
	t.Parallel()

	type configuration struct {
		Labels config.Optional[map[string]string] `config:"labels"`
	}
	plan, err := config.NewPlan(source{
		info: config.SourceInfo{Name: "programmatic"},
		tree: map[string]any{"labels": map[string]any{"region": "eu"}},
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	snapshot, err := config.Load[configuration](context.Background(), plan)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	first, ok := snapshot.Value().Labels.Get()
	if !ok {
		t.Fatal("Optional.Get() ok = false")
	}
	first["region"] = "changed"
	second, ok := snapshot.Value().Labels.Get()
	if !ok || second["region"] != "eu" {
		t.Fatalf("second Optional.Get() = %#v, %v", second, ok)
	}
}

func TestOptionalPreservesWinningDefaultedState(t *testing.T) {
	t.Parallel()

	type configuration struct {
		Count config.Optional[int] `config:"count"`
	}
	defaults, err := programmatic.Defaults("defaults", map[string]any{"count": int64(42)})
	if err != nil {
		t.Fatalf("Defaults() error = %v", err)
	}
	plan, err := config.NewPlan(defaults)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	snapshot, err := config.Load[configuration](context.Background(), plan)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if state := snapshot.Value().Count.State(); state != config.Defaulted {
		t.Fatalf("Optional.State() = %v, want Defaulted", state)
	}
}

func TestOptionalDefaultedStateIsVisibleToValidators(t *testing.T) {
	t.Parallel()

	type configuration struct {
		Nested *struct {
			Count config.Optional[int] `config:"count"`
		} `config:"nested"`
		Missing *config.Optional[int] `config:"missing"`
		Ignored config.Optional[int]  `config:"-"`
	}
	defaults, err := programmatic.Defaults("defaults", map[string]any{
		"nested": map[string]any{"count": int64(42)},
	})
	if err != nil {
		t.Fatalf("Defaults() error = %v", err)
	}
	plan, err := config.NewPlan(defaults)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	validated := false
	snapshot, err := config.LoadWithValidators(
		context.Background(),
		plan,
		func(_ context.Context, value configuration) error {
			validated = true
			if value.Nested.Count.State() != config.Defaulted {
				return validation.At("nested.count", errors.New("not defaulted"))
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("LoadWithValidators() error = %v", err)
	}
	if !validated || snapshot.Value().Nested.Count.State() != config.Defaulted {
		t.Fatalf("validator or snapshot did not observe Defaulted")
	}
}

func TestOptionalRejectsInvalidTypedValueAtomically(t *testing.T) {
	t.Parallel()

	optional := config.Optional[int]{}
	if err := decode.Value("not-an-integer", &optional); err == nil {
		t.Fatal("Value() error = nil")
	}
	if optional.State() != config.Absent {
		t.Fatalf("Optional.State() = %v, want Absent", optional.State())
	}
}
