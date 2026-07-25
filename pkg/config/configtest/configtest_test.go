package configtest_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"strings"
	"testing"

	config "github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/configtest"
)

func TestNewSourceIsDeterministicAndImmutable(t *testing.T) {
	t.Parallel()

	input := map[string]any{"nested": map[string]any{"values": []any{"original"}}}
	source := configtest.NewSource(
		config.SourceInfo{Name: "fixture", Priority: 10},
		config.Document{Tree: input},
	)
	input["nested"].(map[string]any)["values"].([]any)[0] = "mutated"

	first, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	first.Tree["nested"].(map[string]any)["values"].([]any)[0] = "caller"
	second, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := second.Tree["nested"].(map[string]any)["values"].([]any)[0]; got != "original" {
		t.Fatalf("second Load() value = %q, want original", got)
	}
}

func TestFailingSourcePreservesCause(t *testing.T) {
	t.Parallel()

	want := errors.New("fixture failure")
	source := configtest.FailingSource(
		config.SourceInfo{Name: "failure", Priority: 20},
		want,
	)
	_, err := source.Load(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("Load() error = %v, want %v", err, want)
	}
}

func TestEnvironmentReturnsSortedSnapshot(t *testing.T) {
	t.Parallel()

	values := map[string]string{"Z_VALUE": "last", "A_VALUE": "first"}
	got := configtest.Environment(values)
	values["A_VALUE"] = "mutated"
	want := []string{"A_VALUE=first", "Z_VALUE=last"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Environment() = %#v, want %#v", got, want)
	}
}

func TestFilesystemReturnsIndependentReadableFiles(t *testing.T) {
	t.Parallel()

	files := map[string]string{"config/app.json": `{"port":8080}`}
	fixture := configtest.Filesystem(files)
	files["config/app.json"] = "mutated"
	contents, err := fs.ReadFile(fixture, "config/app.json")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := string(contents); got != `{"port":8080}` {
		t.Fatalf("ReadFile() = %q", got)
	}
}

func TestMustLoadAndAssertOrigin(t *testing.T) {
	t.Parallel()

	type settings struct {
		Port int `config:"port"`
	}
	source := configtest.NewSource(
		config.SourceInfo{Name: "fixture", Priority: 10},
		config.Document{Tree: map[string]any{"port": int64(8080)}},
	)
	plan := configtest.MustPlan(t, source)
	snapshot := configtest.MustLoad[settings](t, context.Background(), plan)
	if got := snapshot.Value().Port; got != 8080 {
		t.Fatalf("Port = %d, want 8080", got)
	}
	configtest.AssertOrigin(t, snapshot, "port", config.Origin{
		Source: "fixture", Present: true, State: config.Present,
	})
}

func TestDiffSecretsReportsOnlyRedactedValues(t *testing.T) {
	t.Parallel()

	got := config.NewSecret("canary-got-secret")
	want := config.NewSecret("canary-want-secret")
	diff := configtest.DiffSecrets(got, want)
	if diff == "" {
		t.Fatal("DiffSecrets() = empty for unequal secrets")
	}
	if strings.Contains(diff, got.Reveal()) || strings.Contains(diff, want.Reveal()) {
		t.Fatalf("DiffSecrets() leaked a secret: %q", diff)
	}
	if strings.Count(diff, config.Redacted) != 2 {
		t.Fatalf("DiffSecrets() = %q, want two redaction markers", diff)
	}
	if equal := configtest.DiffSecrets(got, got); equal != "" {
		t.Fatalf("DiffSecrets(equal) = %q, want empty", equal)
	}
}

func TestSourceHonorsCancellationAndClonesOrigins(t *testing.T) {
	t.Parallel()

	source := configtest.NewSource(
		config.SourceInfo{Name: "fixture"},
		config.Document{
			Tree: map[string]any{"value": true},
			Origins: map[string]config.Origin{
				"value": {Location: "fixture.json:1"},
			},
		},
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := source.Load(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Load() error = %v, want context.Canceled", err)
	}
	first, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	first.Origins["value"] = config.Origin{Location: "mutated"}
	second, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if second.Origins["value"].Location != "fixture.json:1" {
		t.Fatalf("second origin = %#v", second.Origins["value"])
	}
}

func TestHelpersReportPlanLoadAndOriginFailures(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		want string
		run  func(configtest.TestingT)
	}{
		"plan": {
			want: "configtest.MustPlan",
			run:  func(t configtest.TestingT) { configtest.MustPlan(t, nil) },
		},
		"load": {
			want: "configtest.MustLoad",
			run: func(t configtest.TestingT) {
				plan, err := config.NewPlan(configtest.FailingSource(
					config.SourceInfo{Name: "failure"}, errors.New("failed"),
				))
				if err != nil {
					panic(err)
				}
				configtest.MustLoad[map[string]any](t, context.Background(), plan)
			},
		},
		"missing origin": {
			want: "has no provenance",
			run: func(t configtest.TestingT) {
				configtest.AssertOrigin(t, originFixture{}, "missing", config.Origin{})
			},
		},
		"different origin": {
			want: "source:\"actual\"",
			run: func(t configtest.TestingT) {
				configtest.AssertOrigin(t, originFixture{
					origin: config.Origin{Source: "actual", Present: true}, found: true,
				}, "value", config.Origin{Source: "wanted"})
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			message := captureFatal(test.run)
			if !strings.Contains(message, test.want) {
				t.Fatalf("fatal message = %q, want %q", message, test.want)
			}
		})
	}
}

type fatalRecorder struct{}

func (fatalRecorder) Helper() {}

func (fatalRecorder) Fatalf(format string, args ...any) {
	panic(fmt.Sprintf(format, args...))
}

func captureFatal(run func(configtest.TestingT)) (message string) {
	defer func() { message = recover().(string) }()
	run(fatalRecorder{})
	return ""
}

type originFixture struct {
	origin config.Origin
	found  bool
}

func (f originFixture) Origin(string) (config.Origin, bool) { return f.origin, f.found }
