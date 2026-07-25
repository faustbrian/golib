package toml_test

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	config "github.com/faustbrian/golib/pkg/config"
	tomlsource "github.com/faustbrian/golib/pkg/config/toml"
)

func TestBytesLoadsDottedKeysAndArrayTables(t *testing.T) {
	t.Parallel()

	source, err := tomlsource.Bytes([]byte(strings.Join([]string{
		`name = "api"`,
		`port = 8080`,
		`ratio = 1.5`,
		`enabled = true`,
		`server.host = "localhost"`,
		`[[workers]]`,
		`name = "first"`,
		`[[workers]]`,
		`name = "second"`,
	}, "\n")), tomlsource.Options{Name: "toml", Priority: 20})
	if err != nil {
		t.Fatalf("Bytes() error = %v", err)
	}
	document, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Source.Load() error = %v", err)
	}
	want := map[string]any{
		"name": "api", "port": int64(8080), "ratio": 1.5, "enabled": true,
		"server": map[string]any{"host": "localhost"},
		"workers": []any{
			map[string]any{"name": "first"},
			map[string]any{"name": "second"},
		},
	}
	if !reflect.DeepEqual(document.Tree, want) {
		t.Fatalf("Source.Load() tree = %#v, want %#v", document.Tree, want)
	}
}

func TestSourceRejectsDuplicateDefinitions(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"simple":       "port = 80\nport = 443\n",
		"dotted":       "server.port = 80\nserver.port = 443\n",
		"table scalar": "server = 1\n[server]\nport = 80\n",
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source, err := tomlsource.Bytes([]byte(data), tomlsource.Options{Name: "toml"})
			if err != nil {
				t.Fatalf("Bytes() error = %v", err)
			}
			if _, err := source.Load(context.Background()); err == nil {
				t.Fatal("Source.Load() error = nil, want duplicate error")
			}
		})
	}
}

func TestParseErrorDoesNotExposeInput(t *testing.T) {
	t.Parallel()

	source, err := tomlsource.Bytes(
		[]byte(`token = "canary-secret-value`),
		tomlsource.Options{Name: "toml", Sensitive: true},
	)
	if err != nil {
		t.Fatalf("Bytes() error = %v", err)
	}
	_, err = source.Load(context.Background())
	if err == nil || strings.Contains(err.Error(), "canary-secret-value") {
		t.Fatalf("Source.Load() error = %v, want redacted parse error", err)
	}
}

func TestSourceEnforcesLimits(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		data   string
		limits tomlsource.Limits
	}{
		"bytes": {data: `long = "value"`, limits: tomlsource.Limits{MaxBytes: 4}},
		"depth": {data: "[one.two.three]\nvalue = true\n", limits: tomlsource.Limits{MaxDepth: 2}},
		"keys":  {data: "one = 1\ntwo = 2\n", limits: tomlsource.Limits{MaxKeys: 1}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source, err := tomlsource.Bytes(
				[]byte(test.data), tomlsource.Options{Name: "toml", Limits: test.limits},
			)
			if err != nil {
				t.Fatalf("Bytes() error = %v", err)
			}
			if _, err := source.Load(context.Background()); err == nil {
				t.Fatal("Source.Load() error = nil, want limit error")
			}
		})
	}
}

func TestFSSourceOptionalOnlySuppressesMissingFile(t *testing.T) {
	t.Parallel()

	missing, err := tomlsource.FromFS(
		fstest.MapFS{}, "missing.toml", tomlsource.Options{Name: "toml", Optional: true},
	)
	if err != nil {
		t.Fatalf("FromFS() error = %v", err)
	}
	_, err = missing.Load(context.Background())
	if !errors.Is(err, config.ErrNotFound) {
		t.Fatalf("Source.Load() error = %v, want ErrNotFound", err)
	}

	malformed, err := tomlsource.FromFS(
		fstest.MapFS{"broken.toml": &fstest.MapFile{Data: []byte(`token = "`)}},
		"broken.toml", tomlsource.Options{Name: "toml", Optional: true},
	)
	if err != nil {
		t.Fatalf("FromFS() error = %v", err)
	}
	_, err = malformed.Load(context.Background())
	if err == nil || errors.Is(err, config.ErrNotFound) {
		t.Fatalf("Source.Load() error = %v, want syntax error", err)
	}
}

func TestFSSourcePreservesPermissionAndCancellation(t *testing.T) {
	t.Parallel()

	source, err := tomlsource.FromFS(permissionFS{}, "secret.toml", tomlsource.Options{Name: "toml"})
	if err != nil {
		t.Fatalf("FromFS() error = %v", err)
	}
	_, err = source.Load(context.Background())
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("Source.Load() error = %v, want fs.ErrPermission", err)
	}

	bytesSource, err := tomlsource.Bytes(nil, tomlsource.Options{Name: "toml"})
	if err != nil {
		t.Fatalf("Bytes() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = bytesSource.Load(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Source.Load() error = %v, want context.Canceled", err)
	}
}

type permissionFS struct{}

func (permissionFS) Open(string) (fs.File, error) { return nil, fs.ErrPermission }
