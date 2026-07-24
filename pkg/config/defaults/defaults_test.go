package defaults_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	config "github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/defaults"
)

type configuration struct {
	Name    string        `config:"name" default:"worker"`
	Port    int           `config:"port" default:"8080"`
	Debug   bool          `config:"debug" default:"false"`
	Timeout time.Duration `config:"timeout" default:"1.5s"`
	Token   config.Secret `config:"token,secret" default:"canary-secret-value"`
	Nested  struct {
		Host string `config:"host" default:"localhost"`
	} `config:"nested"`
	WithoutDefault string `config:"without_default"`
}

func TestForBuildsTypedDefaultSource(t *testing.T) {
	t.Parallel()

	source, err := defaults.For[configuration]("defaults")
	if err != nil {
		t.Fatalf("For() error = %v", err)
	}
	if source.Info().Priority != config.PriorityDefaults {
		t.Fatalf("Source priority = %d", source.Info().Priority)
	}
	plan, err := config.NewPlan(source)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	snapshot, err := config.Load[configuration](context.Background(), plan)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got := snapshot.Value()
	if got.Name != "worker" || got.Port != 8080 || got.Debug || got.Timeout != 1500*time.Millisecond {
		t.Fatalf("Load() value = %#v", got)
	}
	if got.Token.Reveal() != "canary-secret-value" || got.Nested.Host != "localhost" {
		t.Fatalf("Load() nested or secret value = %#v", got)
	}
	if origin, ok := snapshot.Origin("port"); !ok || origin.State != config.Defaulted {
		t.Fatalf("port origin = %#v, %v", origin, ok)
	}
	if origin, ok := snapshot.Origin("token"); !ok || !origin.Sensitive || origin.State != config.Defaulted {
		t.Fatalf("token origin = %#v, %v", origin, ok)
	}
	if _, ok := snapshot.Origin("without_default"); ok {
		t.Fatal("field without default has provenance")
	}
}

func TestForRejectsInvalidDefaultAndDestination(t *testing.T) {
	t.Parallel()

	type invalid struct {
		Port int `config:"port" default:"not-an-integer"`
	}
	if _, err := defaults.For[invalid]("defaults"); err == nil {
		t.Fatal("For() error = nil, want invalid default error")
	} else {
		var typed *defaults.Error
		if !errors.As(err, &typed) {
			t.Fatalf("For() error = %T, want *defaults.Error", err)
		}
		if got := fmt.Sprintf("%#v", typed); got != typed.Error() {
			t.Fatalf("formatted Error = %q", got)
		}
		if text, marshalErr := typed.MarshalText(); marshalErr != nil || string(text) != typed.Error() {
			t.Fatalf("Error.MarshalText() = %q, %v", text, marshalErr)
		}
	}
	if _, err := defaults.For[string]("defaults"); err == nil {
		t.Fatal("For() error = nil, want invalid destination error")
	}
	if _, err := defaults.For[configuration](""); err == nil {
		t.Fatal("For() error = nil, want invalid name error")
	}
}

func TestForRejectsRecursiveSchemaWithoutRecursingIndefinitely(t *testing.T) {
	t.Parallel()

	type node struct {
		Next *node `config:"next"`
	}
	_, err := defaults.For[node]("recursive")
	var schemaErr *defaults.SchemaError
	if !errors.As(err, &schemaErr) || schemaErr.Path != "next" ||
		schemaErr.Reason != "recursive type" {
		t.Fatalf("For() error = %T %#v", err, err)
	}
	if !strings.Contains(schemaErr.Error(), "recursive type") {
		t.Fatalf("SchemaError.Error() = %q", schemaErr)
	}
}

func TestForConvertsOptionalScalarDefault(t *testing.T) {
	t.Parallel()

	type optionalConfiguration struct {
		Count config.Optional[int] `config:"count" default:"42"`
	}
	source, err := defaults.For[optionalConfiguration]("defaults")
	if err != nil {
		t.Fatalf("For() error = %v", err)
	}
	plan, err := config.NewPlan(source)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	snapshot, err := config.Load[optionalConfiguration](context.Background(), plan)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	value, ok := snapshot.Value().Count.Get()
	if !ok || value != 42 {
		t.Fatalf("optional count = %d, %v; want 42, true", value, ok)
	}
}

func TestForConvertsPointerOptionalScalarDefault(t *testing.T) {
	t.Parallel()

	type optionalConfiguration struct {
		Count *config.Optional[int] `config:"count" default:"42"`
	}
	source, err := defaults.For[optionalConfiguration]("defaults")
	if err != nil {
		t.Fatalf("For() error = %v", err)
	}
	plan, err := config.NewPlan(source)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	snapshot, err := config.Load[optionalConfiguration](context.Background(), plan)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	value, ok := snapshot.Value().Count.Get()
	if !ok || value != 42 || snapshot.Value().Count.State() != config.Defaulted {
		t.Fatalf("optional count = %d, %v, %v", value, ok, snapshot.Value().Count.State())
	}
}

func TestForRejectsTrailingCollectionDefaultInput(t *testing.T) {
	t.Parallel()

	type invalid struct {
		Items []int `config:"items" default:"[1] trailing"`
	}
	if _, err := defaults.For[invalid]("defaults"); err == nil {
		t.Fatal("For() error = nil, want trailing input error")
	}
}

func TestDefaultSourceDoesNotExposeNestedCollectionState(t *testing.T) {
	t.Parallel()

	type nestedCollections struct {
		Items []map[string]int `config:"items" default:"[{\"count\":1}]"`
	}
	source, err := defaults.For[nestedCollections]("defaults")
	if err != nil {
		t.Fatalf("For() error = %v", err)
	}
	first, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	first.Tree["items"].([]any)[0].(map[string]any)["count"] = int64(99)
	second, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := []any{map[string]any{"count": int64(1)}}
	if !reflect.DeepEqual(second.Tree["items"], want) {
		t.Fatalf("second Load() items = %#v, want %#v", second.Tree["items"], want)
	}
}

func TestDefaultErrorsAreTypedAndDoNotExposeInput(t *testing.T) {
	t.Parallel()

	type invalid struct {
		Count int `config:"count,secret" default:"canary-secret-value"`
	}
	_, err := defaults.For[invalid]("defaults")
	var defaultErr *defaults.Error
	if !errors.As(err, &defaultErr) {
		t.Fatalf("For() error = %T %v", err, err)
	}
	if defaultErr.Path != "count" || defaultErr.Expected != "int" || defaultErr.Unwrap() == nil {
		t.Fatalf("defaults.Error = %#v", defaultErr)
	}
	if strings.Contains(err.Error(), "canary-secret-value") {
		t.Fatalf("defaults.Error leaked input: %q", err)
	}
	if defaultErr.Cause == nil || strings.Contains(defaultErr.Cause.Error(), "canary-secret-value") ||
		strings.Contains(errors.Unwrap(defaultErr).Error(), "canary-secret-value") {
		t.Fatalf("defaults.Error exposed input through cause: %#v", defaultErr.Cause)
	}
}

func TestForConvertsEverySupportedDefaultShape(t *testing.T) {
	t.Parallel()

	type comprehensive struct {
		ImplicitName string             `default:"worker"`
		Pointer      *int               `config:"pointer" default:"7"`
		Unsigned     uint8              `config:"unsigned" default:"8"`
		Ratio        float32            `config:"ratio" default:"1.5"`
		Endpoint     url.URL            `config:"endpoint" default:"https://example.com/api"`
		Started      time.Time          `config:"started" default:"2026-07-15T08:00:00Z"`
		Limit        config.ByteSize    `config:"limit" default:"2MiB"`
		Items        []int              `config:"items" default:"[1,2]"`
		Labels       map[string]float64 `config:"labels" default:"{\"ratio\":1.5}"`
		Mixed        []any              `config:"mixed" default:"[true,null,\"text\",2.5]"`
		Ignored      string             `config:"-" default:"ignored"`
		Nested       *struct {
			Value string `config:"value" default:"nested"`
		} `config:"nested"`
		Empty   struct{} `config:"empty"`
		private string   `default:"ignored"`
	}
	source, err := defaults.For[*comprehensive]("defaults")
	if err != nil {
		t.Fatalf("For() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := source.Load(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Load() error = %v, want context.Canceled", err)
	}
	plan, err := config.NewPlan(source)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	snapshot, err := config.Load[comprehensive](context.Background(), plan)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got := snapshot.Value()
	if got.ImplicitName != "worker" || got.Pointer == nil || *got.Pointer != 7 ||
		got.Unsigned != 8 || got.Ratio != 1.5 || got.Endpoint.String() != "https://example.com/api" ||
		!got.Started.Equal(time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)) ||
		got.Limit != 2*config.MiB || !reflect.DeepEqual(got.Items, []int{1, 2}) ||
		got.Labels["ratio"] != 1.5 || got.Nested == nil || got.Nested.Value != "nested" {
		t.Fatalf("Load() value = %#v", got)
	}
	wantMixed := []any{true, nil, "text", 2.5}
	if !reflect.DeepEqual(got.Mixed, wantMixed) || got.Ignored != "" || got.private != "" {
		t.Fatalf("Load() mixed/ignored/private = %#v, %q, %q", got.Mixed, got.Ignored, got.private)
	}
	if _, ok := snapshot.Origin("empty"); ok {
		t.Fatal("empty nested struct has provenance")
	}
}

func TestForRejectsInvalidNestedStructCollectionAndUnsupportedDefaults(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		build func() (config.Source, error)
		path  string
	}{
		"nested": {
			build: func() (config.Source, error) {
				type invalid struct {
					Nested struct {
						Count int `config:"count" default:"invalid"`
					} `config:"nested"`
				}
				return defaults.For[invalid]("defaults")
			},
			path: "nested.count",
		},
		"struct": {
			build: func() (config.Source, error) {
				type invalid struct {
					Nested struct{} `config:"nested" default:"invalid"`
				}
				return defaults.For[invalid]("defaults")
			},
			path: "nested",
		},
		"unsupported": {
			build: func() (config.Source, error) {
				type invalid struct {
					Value complex64 `config:"value" default:"1+2i"`
				}
				return defaults.For[invalid]("defaults")
			},
			path: "value",
		},
		"malformed collection": {
			build: func() (config.Source, error) {
				type invalid struct {
					Items []int `config:"items" default:"["`
				}
				return defaults.For[invalid]("defaults")
			},
			path: "items",
		},
		"multiple collections": {
			build: func() (config.Source, error) {
				type invalid struct {
					Items []int `config:"items" default:"[1] [2]"`
				}
				return defaults.For[invalid]("defaults")
			},
			path: "items",
		},
		"array number overflow": {
			build: func() (config.Source, error) {
				type invalid struct {
					Items []int `config:"items" default:"[9223372036854775808]"`
				}
				return defaults.For[invalid]("defaults")
			},
			path: "items",
		},
		"map number overflow": {
			build: func() (config.Source, error) {
				type invalid struct {
					Items map[string]int `config:"items" default:"{\"value\":9223372036854775808}"`
				}
				return defaults.For[invalid]("defaults")
			},
			path: "items",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := test.build()
			var defaultErr *defaults.Error
			if !errors.As(err, &defaultErr) || defaultErr.Path != test.path {
				t.Fatalf("For() error = %T %#v, want path %q", err, err, test.path)
			}
		})
	}
}
