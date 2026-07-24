package environment_test

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
	"github.com/faustbrian/golib/pkg/config/decode"
	"github.com/faustbrian/golib/pkg/config/environment"
	"github.com/faustbrian/golib/pkg/config/programmatic"
)

type mode string

func (m *mode) UnmarshalText(text []byte) error {
	switch string(text) {
	case "safe", "fast":
		*m = mode(text)
		return nil
	default:
		return errors.New("unsupported mode")
	}
}

type configuration struct {
	Port     int               `config:"port,required,secret" env:"PORT"`
	Debug    bool              `config:"debug"`
	Ratio    float64           `config:"ratio"`
	Timeout  time.Duration     `config:"timeout"`
	Started  time.Time         `config:"started"`
	Endpoint url.URL           `config:"endpoint"`
	Limit    config.ByteSize   `config:"limit"`
	Hosts    []string          `config:"hosts"`
	Labels   map[string]string `config:"labels"`
	Mode     mode              `config:"mode"`
	Database struct {
		Host string `config:"host"`
	} `config:"database"`
	Ignored string `config:"ignored" env:"-"`
}

func TestEnvironForMapsAndConvertsTypedFields(t *testing.T) {
	t.Parallel()

	source, err := environment.EnvironFor[configuration](
		[]string{
			"APP_PORT=8080",
			"APP_DEBUG=true",
			"APP_RATIO=1.5",
			"APP_TIMEOUT=1.5s",
			"APP_STARTED=2026-07-15T08:00:00Z",
			"APP_ENDPOINT=https://example.com/api",
			"APP_LIMIT=10MiB",
			`APP_HOSTS=["one","two"]`,
			`APP_LABELS={"region":"eu"}`,
			"APP_MODE=safe",
			"APP_DATABASE__HOST=db.internal",
			"APP_IGNORED=must-not-load",
			"UNRELATED=value",
		},
		environment.Options{
			Name:      "environment",
			Priority:  config.PriorityEnvironment,
			Prefix:    "APP_",
			Separator: "__",
			Case:      environment.CaseSensitive,
		},
	)
	if err != nil {
		t.Fatalf("EnvironFor() error = %v", err)
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
	if got.Port != 8080 || !got.Debug || got.Ratio != 1.5 || got.Timeout != 1500*time.Millisecond {
		t.Fatalf("Load() scalar value = %#v", got)
	}
	if !got.Started.Equal(time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("Load() Started = %v", got.Started)
	}
	if got.Endpoint.String() != "https://example.com/api" || got.Limit != 10*config.MiB {
		t.Fatalf("Load() typed scalar value = %#v", got)
	}
	if !reflect.DeepEqual(got.Hosts, []string{"one", "two"}) || got.Labels["region"] != "eu" {
		t.Fatalf("Load() collection value = %#v", got)
	}
	if got.Mode != "safe" || got.Database.Host != "db.internal" || got.Ignored != "" {
		t.Fatalf("Load() mapped value = %#v", got)
	}
	if origin, ok := snapshot.Origin("port"); !ok || !origin.Sensitive || origin.State != config.Present {
		t.Fatalf("port origin = %#v, %v", origin, ok)
	}
}

func TestEnvironForRejectsMissingRequiredField(t *testing.T) {
	t.Parallel()

	source, err := environment.EnvironFor[configuration](nil, environment.Options{
		Name: "environment", Prefix: "APP_", Separator: "__",
	})
	if err != nil {
		t.Fatalf("EnvironFor() error = %v", err)
	}
	plan, err := config.NewPlan(source)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	_, err = config.Load[configuration](context.Background(), plan)
	var fieldError *decode.FieldError
	if !errors.As(err, &fieldError) {
		t.Fatalf("Load() error = %v, want *decode.FieldError", err)
	}
	if fieldError.Path != "port" || fieldError.Received != "absent" {
		t.Fatalf("FieldError = %#v", fieldError)
	}
}

func TestMappingErrorFormatsAndMarshalsOnlyItsSafeMessage(t *testing.T) {
	t.Parallel()

	err := &environment.MappingError{
		Path: "port", Name: "APP_PORT", Expected: "int", Received: "string",
		Cause: errors.New("canary-secret"),
	}
	if got := fmt.Sprintf("%#v", err); got != err.Error() {
		t.Fatalf("formatted MappingError = %q", got)
	}
	if text, marshalErr := err.MarshalText(); marshalErr != nil || string(text) != err.Error() {
		t.Fatalf("MappingError.MarshalText() = %q, %v", text, marshalErr)
	}
}

func TestLaterSourceMaySupplyRequiredEnvironmentField(t *testing.T) {
	t.Parallel()

	environmentSource, err := environment.EnvironFor[configuration](nil, environment.Options{
		Name: "environment", Prefix: "APP_", Separator: "__",
	})
	if err != nil {
		t.Fatalf("EnvironFor() error = %v", err)
	}
	overrides, err := programmatic.Overrides("overrides", map[string]any{"port": int64(9090)})
	if err != nil {
		t.Fatalf("Overrides() error = %v", err)
	}
	plan, err := config.NewPlan(environmentSource, overrides)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	snapshot, err := config.Load[configuration](context.Background(), plan)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := snapshot.Value().Port; got != 9090 {
		t.Fatalf("Load() port = %d, want 9090", got)
	}
}

func TestEnvironForRejectsDuplicateAndNormalizedCollisions(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		values []string
		mode   environment.CaseMode
	}{
		"duplicate": {
			values: []string{"APP_PORT=1", "APP_PORT=2"},
			mode:   environment.CaseSensitive,
		},
		"case folded": {
			values: []string{"APP_PORT=1", "app_port=2"},
			mode:   environment.CaseInsensitive,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source, err := environment.EnvironFor[configuration](test.values, environment.Options{
				Name: "environment", Prefix: "APP_", Separator: "__", Case: test.mode,
			})
			if err != nil {
				t.Fatalf("EnvironFor() error = %v", err)
			}
			if _, err := source.Load(context.Background()); err == nil {
				t.Fatal("Source.Load() error = nil, want collision error")
			}
		})
	}
}

func TestEnvironForRejectsSchemaCollisions(t *testing.T) {
	t.Parallel()

	type collision struct {
		First  string `config:"first" env:"VALUE"`
		Second string `config:"second" env:"VALUE"`
	}
	_, err := environment.EnvironFor[collision](nil, environment.Options{Name: "environment"})
	if err == nil {
		t.Fatal("EnvironFor() error = nil, want schema collision")
	}
}

func TestEnvironForRejectsRecursiveSchemaWithoutRecursingIndefinitely(t *testing.T) {
	t.Parallel()

	type node struct {
		Next *node `config:"next"`
	}
	source, err := environment.EnvironFor[node](nil, environment.Options{Name: "recursive"})
	var schemaErr *environment.SchemaError
	if source != nil || !errors.As(err, &schemaErr) || schemaErr.Path != "next" ||
		schemaErr.Reason != "recursive type" {
		t.Fatalf("EnvironFor() = %#v, %T %#v", source, err, err)
	}
	if !strings.Contains(schemaErr.Error(), "recursive type") {
		t.Fatalf("SchemaError.Error() = %q", schemaErr)
	}
}

func TestEnvironForDoesNotMutateInput(t *testing.T) {
	t.Parallel()

	values := []string{"APP_PORT=8080"}
	source, err := environment.EnvironFor[configuration](values, environment.Options{
		Name: "environment", Prefix: "APP_", Separator: "__",
	})
	if err != nil {
		t.Fatalf("EnvironFor() error = %v", err)
	}
	values[0] = "APP_PORT=9999"
	document, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Source.Load() error = %v", err)
	}
	if got := document.Tree["port"]; got != int64(8080) {
		t.Fatalf("Source.Load() port = %#v, want 8080", got)
	}
}

func TestEnvironForRejectsMalformedVariableAndValue(t *testing.T) {
	t.Parallel()

	tests := map[string][]string{
		"missing equals":  {"APP_PORT"},
		"invalid integer": {"APP_PORT=not-an-integer"},
	}
	for name, values := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source, err := environment.EnvironFor[configuration](values, environment.Options{
				Name: "environment", Prefix: "APP_", Separator: "__",
			})
			if err != nil {
				t.Fatalf("EnvironFor() error = %v", err)
			}
			if _, err := source.Load(context.Background()); err == nil {
				t.Fatal("Source.Load() error = nil, want malformed value error")
			}
		})
	}
}

func TestEnvironForHonorsCancellation(t *testing.T) {
	t.Parallel()

	source, err := environment.EnvironFor[configuration](
		[]string{"APP_PORT=8080"},
		environment.Options{Name: "environment", Prefix: "APP_", Separator: "__"},
	)
	if err != nil {
		t.Fatalf("EnvironFor() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = source.Load(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Source.Load() error = %v, want context.Canceled", err)
	}
}

func TestEnvironForEnforcesExplicitResourceLimits(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		values []string
		limits environment.Limits
		kind   string
	}{
		"variables": {
			values: []string{"APP_PORT=1", "UNRELATED=2"},
			limits: environment.Limits{MaxVariables: 1, MaxBytes: 100, MaxValueBytes: 100},
			kind:   "variables",
		},
		"total bytes": {
			values: []string{"APP_PORT=123"},
			limits: environment.Limits{MaxVariables: 10, MaxBytes: 5, MaxValueBytes: 100},
			kind:   "bytes",
		},
		"value bytes": {
			values: []string{"APP_PORT=123"},
			limits: environment.Limits{MaxVariables: 10, MaxBytes: 100, MaxValueBytes: 2},
			kind:   "value_bytes",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source, err := environment.EnvironFor[configuration](test.values, environment.Options{
				Name: "environment", Prefix: "APP_", Separator: "__", Limits: test.limits,
			})
			if err != nil {
				t.Fatalf("EnvironFor() error = %v", err)
			}
			_, err = source.Load(context.Background())
			var limitErr *environment.LimitError
			if !errors.As(err, &limitErr) || limitErr.Kind != test.kind {
				t.Fatalf("Source.Load() error = %#v, want limit kind %q", err, test.kind)
			}
		})
	}
}

func TestEnvironForConvertsOptionalScalarValue(t *testing.T) {
	t.Parallel()

	type optionalConfiguration struct {
		Count config.Optional[int] `config:"count" env:"COUNT"`
	}
	source, err := environment.EnvironFor[optionalConfiguration](
		[]string{"APP_COUNT=42"},
		environment.Options{Name: "environment", Prefix: "APP_"},
	)
	if err != nil {
		t.Fatalf("EnvironFor() error = %v", err)
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

func TestEnvironForConvertsPointerOptionalScalarValue(t *testing.T) {
	t.Parallel()

	type optionalConfiguration struct {
		Count *config.Optional[int] `config:"count" env:"COUNT"`
	}
	source, err := environment.EnvironFor[optionalConfiguration](
		[]string{"APP_COUNT=42"},
		environment.Options{Name: "environment", Prefix: "APP_"},
	)
	if err != nil {
		t.Fatalf("EnvironFor() error = %v", err)
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

func TestEnvironForRejectsTrailingCollectionInput(t *testing.T) {
	t.Parallel()

	source, err := environment.EnvironFor[configuration](
		[]string{`APP_HOSTS=["one"] trailing`},
		environment.Options{Name: "environment", Prefix: "APP_"},
	)
	if err != nil {
		t.Fatalf("EnvironFor() error = %v", err)
	}
	if _, err := source.Load(context.Background()); err == nil {
		t.Fatal("Load() error = nil, want trailing collection error")
	}
}

func TestEnvironForConvertsNumericAndBooleanCollections(t *testing.T) {
	t.Parallel()

	type collections struct {
		Counts  []int              `config:"counts"`
		Ratios  map[string]float64 `config:"ratios"`
		Enabled map[string]bool    `config:"enabled"`
	}
	source, err := environment.EnvironFor[collections](
		[]string{
			`APP_COUNTS=[1,2]`,
			`APP_RATIOS={"primary":1.5}`,
			`APP_ENABLED={"feature":true}`,
		},
		environment.Options{Name: "environment", Prefix: "APP_"},
	)
	if err != nil {
		t.Fatalf("EnvironFor() error = %v", err)
	}
	plan, err := config.NewPlan(source)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	snapshot, err := config.Load[collections](context.Background(), plan)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got := snapshot.Value()
	if !reflect.DeepEqual(got.Counts, []int{1, 2}) || got.Ratios["primary"] != 1.5 ||
		!got.Enabled["feature"] {
		t.Fatalf("Load() collections = %#v", got)
	}
}
