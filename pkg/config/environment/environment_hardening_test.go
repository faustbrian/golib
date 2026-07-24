package environment_test

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	config "github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/environment"
)

func TestProcessForReadsFreshProcessSnapshotWithoutMutation(t *testing.T) {
	t.Setenv("GO_CONFIG_TEST_PROCESS_VALUE", "first")

	type settings struct {
		Value string `config:"value" env:"GO_CONFIG_TEST_PROCESS_VALUE"`
	}
	source, err := environment.ProcessFor[*settings](environment.Options{
		Name: "process", Case: environment.CaseSensitive,
	})
	if err != nil {
		t.Fatalf("ProcessFor() error = %v", err)
	}
	first, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if first.Tree["value"] != "first" {
		t.Fatalf("first value = %#v", first.Tree["value"])
	}
	if err := os.Setenv("GO_CONFIG_TEST_PROCESS_VALUE", "second"); err != nil {
		t.Fatalf("Setenv() error = %v", err)
	}
	second, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if second.Tree["value"] != "second" || os.Getenv("GO_CONFIG_TEST_PROCESS_VALUE") != "second" {
		t.Fatalf("second value/process = %#v, %q", second.Tree["value"], os.Getenv("GO_CONFIG_TEST_PROCESS_VALUE"))
	}
}

func TestEnvironForIgnoresUnrelatedWindowsEnvironmentNames(t *testing.T) {
	t.Parallel()

	type settings struct {
		Value string `config:"value" env:"APP_VALUE"`
	}
	source, err := environment.EnvironFor[settings](
		[]string{
			`CommonProgramFiles(x86)=C:\Program Files (x86)\Common Files`,
			`=C:=C:\workspace`,
			"UNRELATED-WITHOUT-VALUE",
			"APP_VALUE=loaded",
		},
		environment.Options{Name: "environment", Case: environment.CaseSensitive},
	)
	if err != nil {
		t.Fatalf("EnvironFor() error = %v", err)
	}
	document, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := document.Tree["value"]; got != "loaded" {
		t.Fatalf("Load() value = %#v, want loaded", got)
	}
}

func TestEnvironForRejectsInvalidOptionsAndSchemas(t *testing.T) {
	t.Parallel()

	type valid struct{ Value string }
	tests := map[string]func() error{
		"empty name": func() error {
			_, err := environment.EnvironFor[valid](nil, environment.Options{})
			return err
		},
		"case": func() error {
			_, err := environment.EnvironFor[valid](nil, environment.Options{
				Name: "environment", Case: environment.CaseMode(99),
			})
			return err
		},
		"negative limit": func() error {
			_, err := environment.EnvironFor[valid](nil, environment.Options{
				Name: "environment", Limits: environment.Limits{MaxBytes: -1},
			})
			return err
		},
		"prefix": func() error {
			_, err := environment.EnvironFor[valid](nil, environment.Options{
				Name: "environment", Prefix: "BAD-",
			})
			return err
		},
		"separator": func() error {
			_, err := environment.EnvironFor[valid](nil, environment.Options{
				Name: "environment", Separator: "--",
			})
			return err
		},
		"destination": func() error {
			_, err := environment.EnvironFor[string](nil, environment.Options{Name: "environment"})
			return err
		},
		"field name": func() error {
			type invalid struct {
				Value string `config:"value" env:"BAD-NAME"`
			}
			_, err := environment.EnvironFor[invalid](nil, environment.Options{Name: "environment"})
			return err
		},
		"nested field name": func() error {
			type invalid struct {
				Nested struct {
					Value string `config:"value" env:"BAD-NAME"`
				} `config:"nested"`
			}
			_, err := environment.EnvironFor[invalid](nil, environment.Options{Name: "environment"})
			return err
		},
	}
	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := run(); err == nil {
				t.Fatal("constructor error = nil")
			}
		})
	}
}

func TestEnvironForMapsImplicitPointerIgnoredAndUnicodeFields(t *testing.T) {
	t.Parallel()

	type settings struct {
		Implicit string
		Pointer  *int   `config:"pointer"`
		Unicode  string `config:"unicode" env:"Å_VALUE"`
		Ignored  string `config:"ignored" env:"-"`
		Skipped  string `config:"-" env:"SKIPPED"`
		private  string
	}
	source, err := environment.EnvironFor[settings](
		[]string{"APP_IMPLICIT=value", "APP_POINTER=42", "APP_Å_VALUE=unicode", "APP_IGNORED=x", "SKIPPED=x"},
		environment.Options{Name: "environment", Prefix: "APP_"},
	)
	if err != nil {
		t.Fatalf("EnvironFor() error = %v", err)
	}
	plan, err := config.NewPlan(source)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	snapshot, err := config.Load[settings](context.Background(), plan)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got := snapshot.Value()
	if got.Implicit != "value" || got.Pointer == nil || *got.Pointer != 42 || got.Unicode != "unicode" ||
		got.Ignored != "" || got.Skipped != "" || got.private != "" {
		t.Fatalf("Load() value = %#v", got)
	}
}

func TestEnvironForHonorsCancellationBetweenEntries(t *testing.T) {
	t.Parallel()

	type settings struct {
		Value string `config:"value" env:"VALUE"`
	}
	source, err := environment.EnvironFor[settings](
		[]string{"VALUE=loaded"}, environment.Options{Name: "environment"},
	)
	if err != nil {
		t.Fatalf("EnvironFor() error = %v", err)
	}
	_, err = source.Load(&stagedContext{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Load() error = %v, want context.Canceled", err)
	}
}

func TestEnvironmentErrorsAreTypedStableAndSecretSafe(t *testing.T) {
	t.Parallel()

	cause := errors.New("canary-secret-value")
	mapping := &environment.MappingError{
		Path: "token", Name: "APP_TOKEN", Expected: "int", Received: "string", Cause: cause,
	}
	if !errors.Is(mapping, cause) || strings.Contains(mapping.Error(), "canary-secret-value") ||
		!strings.Contains(mapping.Error(), "APP_TOKEN") {
		t.Fatalf("MappingError = %q", mapping)
	}
	if unwrapped := errors.Unwrap(mapping); unwrapped == nil ||
		strings.Contains(unwrapped.Error(), "canary-secret-value") {
		t.Fatalf("MappingError.Unwrap() leaked cause: %v", unwrapped)
	}
	limit := &environment.LimitError{Kind: "bytes", Limit: 2, Actual: 3}
	if limit.Error() != "environment bytes limit exceeded: 3 > 2" {
		t.Fatalf("LimitError.Error() = %q", limit)
	}
}

func TestEnvironForConvertsEveryCollectionElementShape(t *testing.T) {
	t.Parallel()

	type settings struct {
		Pointers  []*int          `config:"pointers"`
		Unsigned  []uint8         `config:"unsigned"`
		Durations []time.Duration `config:"durations"`
		Mixed     []any           `config:"mixed"`
	}
	source, err := environment.EnvironFor[settings](
		[]string{
			`APP_POINTERS=[1,2]`, `APP_UNSIGNED=[3,4]`,
			`APP_DURATIONS=["1s","2s"]`, `APP_MIXED=[1,2.5,true,"text"]`,
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
	snapshot, err := config.Load[settings](context.Background(), plan)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got := snapshot.Value()
	if len(got.Pointers) != 2 || *got.Pointers[0] != 1 || *got.Pointers[1] != 2 ||
		!reflect.DeepEqual(got.Unsigned, []uint8{3, 4}) ||
		!reflect.DeepEqual(got.Durations, []time.Duration{time.Second, 2 * time.Second}) ||
		!reflect.DeepEqual(got.Mixed, []any{int64(1), 2.5, true, "text"}) {
		t.Fatalf("Load() value = %#v", got)
	}
}

func TestEnvironForConvertsUnsignedScalar(t *testing.T) {
	t.Parallel()

	type settings struct {
		Value uint8 `config:"value"`
	}
	source, err := environment.EnvironFor[settings](
		[]string{"APP_VALUE=42"}, options(),
	)
	if err != nil {
		t.Fatalf("EnvironFor() error = %v", err)
	}
	plan, err := config.NewPlan(source)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	snapshot, err := config.Load[settings](context.Background(), plan)
	if err != nil || snapshot.Value().Value != 42 {
		t.Fatalf("Load() = %#v, %v", snapshot, err)
	}
}

func TestEnvironForRejectsEveryInvalidCollectionShape(t *testing.T) {
	t.Parallel()

	tests := map[string]func() (config.Source, error){
		"string element": func() (config.Source, error) {
			type schema struct {
				Values []string `config:"values"`
			}
			return environment.EnvironFor[schema]([]string{`APP_VALUES=[1]`}, options())
		},
		"boolean element": func() (config.Source, error) {
			type schema struct {
				Values []bool `config:"values"`
			}
			return environment.EnvironFor[schema]([]string{`APP_VALUES=["true"]`}, options())
		},
		"integer element": func() (config.Source, error) {
			type schema struct {
				Values []int `config:"values"`
			}
			return environment.EnvironFor[schema]([]string{`APP_VALUES=[true]`}, options())
		},
		"unsigned element": func() (config.Source, error) {
			type schema struct {
				Values []uint8 `config:"values"`
			}
			return environment.EnvironFor[schema]([]string{`APP_VALUES=[-1]`}, options())
		},
		"unsigned element type": func() (config.Source, error) {
			type schema struct {
				Values []uint8 `config:"values"`
			}
			return environment.EnvironFor[schema]([]string{`APP_VALUES=[true]`}, options())
		},
		"float element": func() (config.Source, error) {
			type schema struct {
				Values []float64 `config:"values"`
			}
			return environment.EnvironFor[schema]([]string{`APP_VALUES=[true]`}, options())
		},
		"array shape": func() (config.Source, error) {
			type schema struct {
				Values []string `config:"values"`
			}
			return environment.EnvironFor[schema]([]string{`APP_VALUES={}`}, options())
		},
		"map keys": func() (config.Source, error) {
			type schema struct {
				Values map[int]string `config:"values"`
			}
			return environment.EnvironFor[schema]([]string{`APP_VALUES={"1":"one"}`}, options())
		},
		"map shape": func() (config.Source, error) {
			type schema struct {
				Values map[string]string `config:"values"`
			}
			return environment.EnvironFor[schema]([]string{`APP_VALUES=[]`}, options())
		},
		"scalar hook": func() (config.Source, error) {
			type schema struct {
				Values []time.Duration `config:"values"`
			}
			return environment.EnvironFor[schema]([]string{`APP_VALUES=[1]`}, options())
		},
		"unsupported": func() (config.Source, error) {
			type schema struct {
				Values []complex64 `config:"values"`
			}
			return environment.EnvironFor[schema]([]string{`APP_VALUES=["1+2i"]`}, options())
		},
		"nested slice failure": func() (config.Source, error) {
			type schema struct {
				Values [][]int `config:"values"`
			}
			return environment.EnvironFor[schema]([]string{`APP_VALUES=[[true]]`}, options())
		},
		"nested map failure": func() (config.Source, error) {
			type schema struct {
				Values map[string]int `config:"values"`
			}
			return environment.EnvironFor[schema]([]string{`APP_VALUES={"bad":true}`}, options())
		},
	}
	for name, build := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source, err := build()
			if err != nil {
				t.Fatalf("constructor error = %v", err)
			}
			_, err = source.Load(context.Background())
			var mapping *environment.MappingError
			if !errors.As(err, &mapping) {
				t.Fatalf("Load() error = %T %v", err, err)
			}
		})
	}
}

func TestEnvironForRejectsScalarAndCollectionConversionFailures(t *testing.T) {
	t.Parallel()

	tests := map[string]func() (config.Source, error){
		"unsigned": func() (config.Source, error) {
			type schema struct {
				Value uint8 `config:"value"`
			}
			return environment.EnvironFor[schema]([]string{"APP_VALUE=-1"}, options())
		},
		"unsupported": func() (config.Source, error) {
			type schema struct {
				Value complex64 `config:"value"`
			}
			return environment.EnvironFor[schema]([]string{"APP_VALUE=1+2i"}, options())
		},
		"malformed JSON": func() (config.Source, error) {
			type schema struct {
				Value []int `config:"value"`
			}
			return environment.EnvironFor[schema]([]string{"APP_VALUE=["}, options())
		},
		"multiple JSON values": func() (config.Source, error) {
			type schema struct {
				Value []int `config:"value"`
			}
			return environment.EnvironFor[schema]([]string{"APP_VALUE=[1] [2]"}, options())
		},
	}
	for name, build := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source, err := build()
			if err != nil {
				t.Fatalf("constructor error = %v", err)
			}
			if _, err := source.Load(context.Background()); err == nil {
				t.Fatal("Load() error = nil")
			}
		})
	}
}

func TestEnvironForIgnoresUnrelatedPlatformVariableNames(t *testing.T) {
	t.Parallel()

	type settings struct {
		Value string `config:"value"`
	}
	for _, entry := range []string{"=value", "1BAD=value", "BAD-NAME=value"} {
		source, err := environment.EnvironFor[settings]([]string{entry}, options())
		if err != nil {
			t.Fatalf("EnvironFor() error = %v", err)
		}
		document, err := source.Load(context.Background())
		if err != nil || len(document.Tree) != 0 {
			t.Fatalf("Load(%q) = %#v, %v; want empty tree", entry, document.Tree, err)
		}
	}
}

func options() environment.Options {
	return environment.Options{Name: "environment", Prefix: "APP_"}
}

type stagedContext struct{ calls int }

func (c *stagedContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *stagedContext) Done() <-chan struct{}       { return nil }
func (c *stagedContext) Value(any) any               { return nil }
func (c *stagedContext) Err() error {
	c.calls++
	if c.calls > 1 {
		return context.Canceled
	}
	return nil
}
