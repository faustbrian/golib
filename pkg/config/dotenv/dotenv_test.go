package dotenv_test

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	config "github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/dotenv"
	"github.com/faustbrian/golib/pkg/config/environment"
)

type configuration struct {
	Plain     string `config:"plain" env:"PLAIN"`
	Single    string `config:"single" env:"SINGLE"`
	Double    string `config:"double" env:"DOUBLE"`
	Multiline string `config:"multiline" env:"MULTILINE"`
	Empty     string `config:"empty" env:"EMPTY"`
	Port      int    `config:"port" env:"PORT"`
	Escaped   string `config:"escaped" env:"ESCAPED"`
}

func TestBytesParsesDocumentedDotenvGrammar(t *testing.T) {
	t.Parallel()

	source, err := dotenv.BytesFor[configuration]([]byte(strings.Join([]string{
		"# comment",
		"export PLAIN=value # inline comment",
		"SINGLE='literal # ${NAME}'",
		`DOUBLE="line\nquote:\" slash:\\"`,
		`MULTILINE="first`,
		`second"`,
		"EMPTY=",
		"PORT=8080",
		"ESCAPED=hash\\#value",
		"",
	}, "\n")), dotenv.Options{Name: "dotenv"})
	if err != nil {
		t.Fatalf("BytesFor() error = %v", err)
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
	if got.Plain != "value" || got.Single != "literal # ${NAME}" || got.Empty != "" || got.Port != 8080 {
		t.Fatalf("Load() basic value = %#v", got)
	}
	if got.Double != "line\nquote:\" slash:\\" || got.Multiline != "first\nsecond" {
		t.Fatalf("Load() quoted value = %#v", got)
	}
	if got.Escaped != "hash#value" {
		t.Fatalf("Load() escaped value = %q", got.Escaped)
	}
}

func TestBytesRejectsDuplicateKeys(t *testing.T) {
	t.Parallel()

	source, err := dotenv.BytesFor[configuration](
		[]byte("PORT=8080\nPORT=9090\n"),
		dotenv.Options{Name: "dotenv"},
	)
	if err != nil {
		t.Fatalf("BytesFor() error = %v", err)
	}
	_, err = source.Load(context.Background())
	var duplicate *dotenv.DuplicateKeyError
	if !errors.As(err, &duplicate) {
		t.Fatalf("Source.Load() error = %v, want *DuplicateKeyError", err)
	}
	if duplicate.Name != "PORT" || duplicate.FirstLine != 1 || duplicate.Line != 2 {
		t.Fatalf("DuplicateKeyError = %#v", duplicate)
	}
}

func TestInterpolationIsExplicitBoundedAndCycleDetected(t *testing.T) {
	t.Parallel()

	t.Run("disabled", func(t *testing.T) {
		t.Parallel()
		source, err := dotenv.BytesFor[configuration](
			[]byte("PLAIN=${NAME}\n"),
			dotenv.Options{Name: "dotenv"},
		)
		if err != nil {
			t.Fatalf("BytesFor() error = %v", err)
		}
		document, err := source.Load(context.Background())
		if err != nil {
			t.Fatalf("Source.Load() error = %v", err)
		}
		if got := document.Tree["plain"]; got != "${NAME}" {
			t.Fatalf("plain = %q, want literal interpolation", got)
		}
	})

	t.Run("enabled", func(t *testing.T) {
		t.Parallel()
		source, err := dotenv.BytesFor[configuration](
			[]byte(strings.Join([]string{
				"NAME=worker",
				"PLAIN=${NAME}-${SUFFIX:-default}",
				`ESCAPED=\${NAME}`,
			}, "\n")),
			dotenv.Options{
				Name: "dotenv",
				Interpolation: &dotenv.Interpolation{
					IncludeFile: true, MaxDepth: 8, MaxExpandedBytes: 1024,
				},
			},
		)
		if err != nil {
			t.Fatalf("BytesFor() error = %v", err)
		}
		document, err := source.Load(context.Background())
		if err != nil {
			t.Fatalf("Source.Load() error = %v", err)
		}
		if document.Tree["plain"] != "worker-default" || document.Tree["escaped"] != "${NAME}" {
			t.Fatalf("Source.Load() tree = %#v", document.Tree)
		}
	})

	t.Run("cycle", func(t *testing.T) {
		t.Parallel()
		source, err := dotenv.BytesFor[configuration](
			[]byte("PLAIN=${ESCAPED}\nESCAPED=${PLAIN}\n"),
			dotenv.Options{
				Name:          "dotenv",
				Interpolation: &dotenv.Interpolation{IncludeFile: true, MaxDepth: 8},
			},
		)
		if err != nil {
			t.Fatalf("BytesFor() error = %v", err)
		}
		if _, err := source.Load(context.Background()); err == nil {
			t.Fatal("Source.Load() error = nil, want interpolation cycle")
		}
	})

	t.Run("missing", func(t *testing.T) {
		t.Parallel()
		source, err := dotenv.BytesFor[configuration](
			[]byte("PLAIN=${MISSING}\n"),
			dotenv.Options{Name: "dotenv", Interpolation: &dotenv.Interpolation{}},
		)
		if err != nil {
			t.Fatalf("BytesFor() error = %v", err)
		}
		if _, err := source.Load(context.Background()); err == nil {
			t.Fatal("Source.Load() error = nil, want missing interpolation error")
		}
	})
}

func TestSourceEnforcesLimits(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		data   string
		limits dotenv.Limits
	}{
		"bytes":      {data: "PLAIN=value\n", limits: dotenv.Limits{MaxBytes: 4}},
		"lines":      {data: "PLAIN=value\nEMPTY=\n", limits: dotenv.Limits{MaxLines: 1}},
		"line bytes": {data: "PLAIN=value\n", limits: dotenv.Limits{MaxLineBytes: 4}},
		"keys":       {data: "PLAIN=value\nEMPTY=\n", limits: dotenv.Limits{MaxKeys: 1}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source, err := dotenv.BytesFor[configuration](
				[]byte(test.data), dotenv.Options{Name: "dotenv", Limits: test.limits},
			)
			if err != nil {
				t.Fatalf("BytesFor() error = %v", err)
			}
			if _, err := source.Load(context.Background()); err == nil {
				t.Fatal("Source.Load() error = nil, want limit error")
			}
		})
	}
}

func TestFSSourceOptionalOnlySuppressesMissingFile(t *testing.T) {
	t.Parallel()

	missing, err := dotenv.FromFSFor[configuration](
		fstest.MapFS{}, "missing.env", dotenv.Options{Name: "dotenv", Optional: true},
	)
	if err != nil {
		t.Fatalf("FromFSFor() error = %v", err)
	}
	_, err = missing.Load(context.Background())
	if !errors.Is(err, config.ErrNotFound) {
		t.Fatalf("Source.Load() error = %v, want ErrNotFound", err)
	}

	malformed, err := dotenv.FromFSFor[configuration](
		fstest.MapFS{"broken.env": &fstest.MapFile{Data: []byte("BROKEN")}},
		"broken.env", dotenv.Options{Name: "dotenv", Optional: true},
	)
	if err != nil {
		t.Fatalf("FromFSFor() error = %v", err)
	}
	_, err = malformed.Load(context.Background())
	if err == nil || errors.Is(err, config.ErrNotFound) {
		t.Fatalf("Source.Load() error = %v, want syntax error", err)
	}
}

func TestFSSourcePreservesPermissionErrors(t *testing.T) {
	t.Parallel()

	source, err := dotenv.FromFSFor[configuration](
		permissionFS{}, "secret.env", dotenv.Options{Name: "dotenv"},
	)
	if err != nil {
		t.Fatalf("FromFSFor() error = %v", err)
	}
	_, err = source.Load(context.Background())
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("Source.Load() error = %v, want fs.ErrPermission", err)
	}
}

func TestEnvironmentWinsOverDotenvByDefault(t *testing.T) {
	t.Parallel()

	dotenvSource, err := dotenv.BytesFor[configuration](
		[]byte("PORT=8080\n"), dotenv.Options{Name: "dotenv"},
	)
	if err != nil {
		t.Fatalf("BytesFor() error = %v", err)
	}
	environmentSource, err := environment.EnvironFor[configuration](
		[]string{"PORT=9090"}, environment.Options{
			Name: "environment", Priority: config.PriorityEnvironment,
		},
	)
	if err != nil {
		t.Fatalf("EnvironFor() error = %v", err)
	}
	plan, err := config.NewPlan(environmentSource, dotenvSource)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	snapshot, err := config.Load[configuration](context.Background(), plan)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := snapshot.Value().Port; got != 9090 {
		t.Fatalf("Load() port = %d, want process environment winner", got)
	}
}

func TestSourceHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	source, err := dotenv.BytesFor[configuration](nil, dotenv.Options{Name: "dotenv"})
	if err != nil {
		t.Fatalf("BytesFor() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = source.Load(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Source.Load() error = %v, want context.Canceled", err)
	}
}

type permissionFS struct{}

func (permissionFS) Open(string) (fs.File, error) { return nil, fs.ErrPermission }
