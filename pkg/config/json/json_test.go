package json_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	config "github.com/faustbrian/golib/pkg/config"
	jsonsource "github.com/faustbrian/golib/pkg/config/json"
)

func TestBytesLoadsStrictTree(t *testing.T) {
	t.Parallel()

	source, err := jsonsource.Bytes(
		[]byte(`{"name":"api","port":8080,"ratio":1.5,"enabled":true,"items":["one"]}`),
		jsonsource.Options{Name: "base", Priority: 20},
	)
	if err != nil {
		t.Fatalf("Bytes() error = %v", err)
	}

	document, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Source.Load() error = %v", err)
	}
	want := map[string]any{
		"name": "api", "port": int64(8080), "ratio": 1.5,
		"enabled": true, "items": []any{"one"},
	}
	if !reflect.DeepEqual(document.Tree, want) {
		t.Fatalf("Source.Load() tree = %#v, want %#v", document.Tree, want)
	}
	if got := source.Info(); got.Name != "base" || got.Priority != 20 {
		t.Fatalf("Source.Info() = %#v", got)
	}
}

func TestBytesRejectsInvalidOptions(t *testing.T) {
	t.Parallel()

	tests := map[string]jsonsource.Options{
		"empty name": {},
		"zero bytes": {Name: "json", Limits: jsonsource.Limits{MaxBytes: -1}},
		"zero depth": {Name: "json", Limits: jsonsource.Limits{MaxDepth: -1}},
		"zero keys":  {Name: "json", Limits: jsonsource.Limits{MaxKeys: -1}},
	}
	for name, options := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := jsonsource.Bytes(nil, options); err == nil {
				t.Fatal("Bytes() error = nil, want invalid options error")
			}
		})
	}
}

func TestSourceRejectsDuplicateKeys(t *testing.T) {
	t.Parallel()

	source, err := jsonsource.Bytes(
		[]byte(`{"server":{"port":80,"port":443}}`),
		jsonsource.Options{Name: "json"},
	)
	if err != nil {
		t.Fatalf("Bytes() error = %v", err)
	}

	_, err = source.Load(context.Background())
	var duplicate *jsonsource.DuplicateKeyError
	if !errors.As(err, &duplicate) {
		t.Fatalf("Source.Load() error = %v, want *DuplicateKeyError", err)
	}
	if duplicate.Path != "server.port" {
		t.Fatalf("DuplicateKeyError.Path = %q, want server.port", duplicate.Path)
	}
}

func TestOversizedNumberDoesNotExposeSensitiveToken(t *testing.T) {
	t.Parallel()

	const canary = "12345678901234567890123456789012345678901234567890"
	source, err := jsonsource.Bytes(
		[]byte(`{"token":`+canary+`}`),
		jsonsource.Options{Name: "secret-json", Sensitive: true},
	)
	if err != nil {
		t.Fatalf("Bytes() error = %v", err)
	}
	_, err = source.Load(context.Background())
	if err == nil {
		t.Fatal("Load() error = nil")
	}
	for _, formatted := range []string{
		err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err),
		fmt.Sprintf("%#v", err),
	} {
		if strings.Contains(formatted, canary) {
			t.Fatalf("Load() error leaked numeric token: %q", formatted)
		}
	}
}

func TestSourceEnforcesLimits(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		data   string
		limits jsonsource.Limits
	}{
		"bytes": {data: `{"long":"value"}`, limits: jsonsource.Limits{MaxBytes: 4}},
		"depth": {data: `{"one":{"two":{"three":true}}}`, limits: jsonsource.Limits{MaxDepth: 2}},
		"keys":  {data: `{"one":1,"two":2}`, limits: jsonsource.Limits{MaxKeys: 1}},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source, err := jsonsource.Bytes(
				[]byte(test.data),
				jsonsource.Options{Name: name, Limits: test.limits},
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

	missing, err := jsonsource.FromFS(
		fstest.MapFS{},
		"missing.json",
		jsonsource.Options{Name: "optional", Optional: true},
	)
	if err != nil {
		t.Fatalf("FromFS() error = %v", err)
	}
	_, err = missing.Load(context.Background())
	if !errors.Is(err, config.ErrNotFound) {
		t.Fatalf("Source.Load() error = %v, want ErrNotFound", err)
	}

	malformed, err := jsonsource.FromFS(
		fstest.MapFS{"broken.json": &fstest.MapFile{Data: []byte(`{"broken":`)}},
		"broken.json",
		jsonsource.Options{Name: "optional", Optional: true},
	)
	if err != nil {
		t.Fatalf("FromFS() error = %v", err)
	}
	_, err = malformed.Load(context.Background())
	if err == nil || errors.Is(err, config.ErrNotFound) {
		t.Fatalf("Source.Load() error = %v, want malformed error", err)
	}
}

func TestFSSourcePreservesPermissionErrors(t *testing.T) {
	t.Parallel()

	source, err := jsonsource.FromFS(
		permissionFS{},
		"secret.json",
		jsonsource.Options{Name: "required"},
	)
	if err != nil {
		t.Fatalf("FromFS() error = %v", err)
	}
	_, err = source.Load(context.Background())
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("Source.Load() error = %v, want fs.ErrPermission", err)
	}
}

func TestSourceHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	source, err := jsonsource.Bytes([]byte(`{"name":"api"}`), jsonsource.Options{Name: "json"})
	if err != nil {
		t.Fatalf("Bytes() error = %v", err)
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
