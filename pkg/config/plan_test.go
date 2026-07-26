package config_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	config "github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/decode"
	"github.com/faustbrian/golib/pkg/config/validation"
)

type source struct {
	info    config.SourceInfo
	tree    map[string]any
	origins map[string]config.Origin
	err     error
}

type typedNilSource struct{}

func (*typedNilSource) Info() config.SourceInfo {
	panic("typed nil source metadata must not be called")
}

func (*typedNilSource) Load(context.Context) (config.Document, error) {
	panic("typed nil source load must not be called")
}

type typedConfiguration struct {
	Name string `config:"name"`
	Port int    `config:"port"`
}

type selfValidatingConfiguration struct {
	Name string `config:"name"`
}

func (c selfValidatingConfiguration) Validate() error {
	if c.Name == "invalid" {
		return errors.New("canary-secret-value")
	}
	return nil
}

func (s source) Info() config.SourceInfo { return s.info }

func (s source) Load(context.Context) (config.Document, error) {
	return config.Document{Tree: s.tree, Origins: s.origins}, s.err
}

func TestNewPlanOrdersSourcesByPrecedence(t *testing.T) {
	t.Parallel()

	plan, err := config.NewPlan(
		source{info: config.SourceInfo{Name: "environment", Priority: 60}},
		source{info: config.SourceInfo{Name: "defaults", Priority: 10}},
		source{info: config.SourceInfo{Name: "dotenv", Priority: 50}},
	)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}

	got := plan.Sources()
	want := []config.SourceInfo{
		{Name: "defaults", Priority: 10},
		{Name: "dotenv", Priority: 50},
		{Name: "environment", Priority: 60},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Plan.Sources() = %#v, want %#v", got, want)
	}
}

func TestNewPlanPreservesCallerOrderAtEqualPriority(t *testing.T) {
	t.Parallel()

	plan, err := config.NewPlan(
		source{info: config.SourceInfo{Name: "first", Priority: 40}},
		source{info: config.SourceInfo{Name: "second", Priority: 40}},
	)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}

	got := plan.Sources()
	if got[0].Name != "first" || got[1].Name != "second" {
		t.Fatalf("Plan.Sources() order = %q, %q", got[0].Name, got[1].Name)
	}
}

func TestNewPlanRejectsInvalidSourceMetadata(t *testing.T) {
	t.Parallel()

	tests := map[string][]config.Source{
		"empty name": {
			source{info: config.SourceInfo{Priority: 10}},
		},
		"duplicate name": {
			source{info: config.SourceInfo{Name: "same", Priority: 10}},
			source{info: config.SourceInfo{Name: "same", Priority: 20}},
		},
		"nil source": {nil},
	}

	for name, sources := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := config.NewPlan(sources...)
			if err == nil {
				t.Fatal("NewPlan() error = nil, want error")
			}
		})
	}
}

func TestNewPlanRejectsTypedNilSourceWithoutPanicking(t *testing.T) {
	t.Parallel()

	var source *typedNilSource
	if _, err := config.NewPlan(source); err == nil {
		t.Fatal("NewPlan() error = nil, want typed nil error")
	}
}

func TestLoadTreeIsAtomicAndTracksWinningSource(t *testing.T) {
	t.Parallel()

	plan, err := config.NewPlan(
		source{
			info: config.SourceInfo{Name: "defaults", Priority: 10},
			tree: map[string]any{"server": map[string]any{"host": "localhost", "port": int64(80)}},
		},
		source{
			info: config.SourceInfo{Name: "environment", Priority: 60, Sensitive: true},
			tree: map[string]any{"server": map[string]any{"port": int64(443)}},
		},
	)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}

	snapshot, err := config.LoadTree(context.Background(), plan)
	if err != nil {
		t.Fatalf("LoadTree() error = %v", err)
	}

	want := map[string]any{"server": map[string]any{"host": "localhost", "port": int64(443)}}
	if got := snapshot.Value(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Snapshot.Value() = %#v, want %#v", got, want)
	}

	origin, ok := snapshot.Origin("server.port")
	if !ok {
		t.Fatal("Snapshot.Origin() ok = false, want true")
	}
	if origin.Source != "environment" || !origin.Sensitive || !origin.Present {
		t.Fatalf("Snapshot.Origin() = %#v", origin)
	}
}

func TestLoadTreeReturnsNoSnapshotWhenLaterSourceFails(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("malformed")
	plan, err := config.NewPlan(
		source{info: config.SourceInfo{Name: "valid", Priority: 10}, tree: map[string]any{"value": "loaded"}},
		source{info: config.SourceInfo{Name: "invalid", Priority: 20}, err: wantErr},
	)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}

	snapshot, err := config.LoadTree(context.Background(), plan)
	if snapshot != nil {
		t.Fatalf("LoadTree() snapshot = %#v, want nil", snapshot)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("LoadTree() error = %v, want wrapping %v", err, wantErr)
	}
}

func TestSnapshotDoesNotExposeMutableState(t *testing.T) {
	t.Parallel()

	input := map[string]any{"items": []any{"original"}}
	plan, err := config.NewPlan(source{
		info: config.SourceInfo{Name: "programmatic", Priority: 70},
		tree: input,
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}

	snapshot, err := config.LoadTree(context.Background(), plan)
	if err != nil {
		t.Fatalf("LoadTree() error = %v", err)
	}

	input["items"].([]any)[0] = "source changed"
	first := snapshot.Value()
	first["items"].([]any)[0] = "caller changed"

	if got := snapshot.Value()["items"].([]any)[0]; got != "original" {
		t.Fatalf("Snapshot.Value() item = %q, want original", got)
	}
}

func TestTypedSnapshotClonesExportedFieldsWhenStructHasPrivateState(t *testing.T) {
	t.Parallel()

	type settings struct {
		Labels  map[string]string `config:"labels"`
		private string
	}
	plan, err := config.NewPlan(source{
		info: config.SourceInfo{Name: "fixture"},
		tree: map[string]any{"labels": map[string]any{"region": "eu"}},
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	snapshot, err := config.Load[settings](context.Background(), plan)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	first := snapshot.Value()
	first.Labels["region"] = "mutated"
	first.private = "mutated"
	second := snapshot.Value()
	if second.Labels["region"] != "eu" || second.private != "" {
		t.Fatalf("second Snapshot.Value() = %#v, want isolated exported and private state", second)
	}
}

func TestLoadDecodesTypedImmutableSnapshot(t *testing.T) {
	t.Parallel()

	plan, err := config.NewPlan(source{
		info: config.SourceInfo{Name: "programmatic", Priority: 70},
		tree: map[string]any{"name": "api", "port": int64(8080)},
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}

	snapshot, err := config.Load[typedConfiguration](context.Background(), plan)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	first := snapshot.Value()
	first.Name = "changed"
	if got := snapshot.Value(); got.Name != "api" || got.Port != 8080 {
		t.Fatalf("Snapshot.Value() = %#v", got)
	}
}

func TestLoadReturnsNoTypedSnapshotOnDecodeFailure(t *testing.T) {
	t.Parallel()

	plan, err := config.NewPlan(source{
		info: config.SourceInfo{Name: "programmatic", Priority: 70},
		tree: map[string]any{"port": "not-an-integer"},
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}

	snapshot, err := config.Load[typedConfiguration](context.Background(), plan)
	if err == nil {
		t.Fatal("Load() error = nil, want decode error")
	}
	if snapshot != nil {
		t.Fatalf("Load() snapshot = %#v, want nil", snapshot)
	}
}

func TestLoadDecodeErrorIncludesWinningSourceAndLocation(t *testing.T) {
	t.Parallel()

	plan, err := config.NewPlan(source{
		info: config.SourceInfo{Name: "settings", Priority: 40},
		tree: map[string]any{"port": "not-an-integer"},
		origins: map[string]config.Origin{
			"port": {Location: "/etc/example/settings.yaml:4"},
		},
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}

	_, err = config.Load[typedConfiguration](context.Background(), plan)
	var fieldErr *decode.FieldError
	if !errors.As(err, &fieldErr) {
		t.Fatalf("expected FieldError, got %T: %v", err, err)
	}
	if fieldErr.Source != "settings" || fieldErr.Location != "/etc/example/settings.yaml:4" {
		t.Fatalf("FieldError source context = %#v", fieldErr)
	}
	if !strings.Contains(err.Error(), `source "settings"`) ||
		!strings.Contains(err.Error(), `/etc/example/settings.yaml:4`) {
		t.Fatalf("error = %q, want safe source context", err)
	}
}

func TestLoadRunsSelfValidationWithoutReturningFailedSnapshot(t *testing.T) {
	t.Parallel()

	plan, err := config.NewPlan(source{
		info: config.SourceInfo{Name: "programmatic", Priority: 70},
		tree: map[string]any{"name": "invalid"},
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}

	snapshot, err := config.Load[selfValidatingConfiguration](context.Background(), plan)
	if snapshot != nil {
		t.Fatalf("Load() snapshot = %#v, want nil", snapshot)
	}
	if err == nil || strings.Contains(err.Error(), "canary-secret-value") {
		t.Fatalf("Load() error = %v, want redacted validation error", err)
	}
}

func TestLoadWithValidatorsAggregatesAfterCompleteDecode(t *testing.T) {
	t.Parallel()

	plan, err := config.NewPlan(source{
		info: config.SourceInfo{Name: "programmatic", Priority: 70},
		tree: map[string]any{"name": "api", "port": int64(8080)},
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}

	first := errors.New("first")
	second := errors.New("second")
	snapshot, err := config.LoadWithValidators(
		context.Background(),
		plan,
		func(context.Context, typedConfiguration) error { return validation.At("name", first) },
		func(context.Context, typedConfiguration) error { return validation.At("port", second) },
	)
	if snapshot != nil {
		t.Fatalf("LoadWithValidators() snapshot = %#v, want nil", snapshot)
	}
	if !errors.Is(err, first) || !errors.Is(err, second) {
		t.Fatalf("LoadWithValidators() error = %v, want both causes", err)
	}
}
