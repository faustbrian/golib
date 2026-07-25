package yaml_test

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
	yamlsource "github.com/faustbrian/golib/pkg/config/yaml"
)

func TestBytesLoadsStrictTree(t *testing.T) {
	t.Parallel()

	source, err := yamlsource.Bytes([]byte(strings.Join([]string{
		"name: api",
		"port: 8080",
		"ratio: 1.5",
		"enabled: true",
		"empty: ''",
		"nothing: null",
		"items:",
		"  - one",
		"nested:",
		"  host: localhost",
	}, "\n")), yamlsource.Options{Name: "yaml", Priority: 20})
	if err != nil {
		t.Fatalf("Bytes() error = %v", err)
	}
	document, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Source.Load() error = %v", err)
	}
	want := map[string]any{
		"name": "api", "port": int64(8080), "ratio": 1.5,
		"enabled": true, "empty": "", "nothing": nil,
		"items": []any{"one"}, "nested": map[string]any{"host": "localhost"},
	}
	if !reflect.DeepEqual(document.Tree, want) {
		t.Fatalf("Source.Load() tree = %#v, want %#v", document.Tree, want)
	}
}

func TestSourceRejectsAmbiguousOrExecutableFeatures(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"duplicate":      "server:\n  port: 80\n  port: 443\n",
		"alias":          "base: &base\n  port: 80\nserver: *base\n",
		"merge key":      "base: &base\n  port: 80\nserver:\n  <<: *base\n",
		"custom tag":     "value: !custom payload\n",
		"non-string key": "1: value\n",
		"multiple docs":  "name: one\n---\nname: two\n",
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source, err := yamlsource.Bytes([]byte(data), yamlsource.Options{Name: "yaml"})
			if err != nil {
				t.Fatalf("Bytes() error = %v", err)
			}
			if _, err := source.Load(context.Background()); err == nil {
				t.Fatal("Source.Load() error = nil, want strict YAML error")
			}
		})
	}
}

func TestDuplicateErrorIncludesSafePathAndLocation(t *testing.T) {
	t.Parallel()

	source, err := yamlsource.Bytes(
		[]byte("server:\n  port: 80\n  port: 443\n"),
		yamlsource.Options{Name: "yaml"},
	)
	if err != nil {
		t.Fatalf("Bytes() error = %v", err)
	}
	_, err = source.Load(context.Background())
	var duplicate *yamlsource.DuplicateKeyError
	if !errors.As(err, &duplicate) {
		t.Fatalf("Source.Load() error = %v, want *DuplicateKeyError", err)
	}
	if duplicate.Path != "server.port" || duplicate.Line != 3 || duplicate.Column != 3 {
		t.Fatalf("DuplicateKeyError = %#v", duplicate)
	}
}

func TestParseErrorDoesNotExposeInput(t *testing.T) {
	t.Parallel()

	source, err := yamlsource.Bytes(
		[]byte("token: \"canary-secret-value\n"),
		yamlsource.Options{Name: "yaml", Sensitive: true},
	)
	if err != nil {
		t.Fatalf("Bytes() error = %v", err)
	}
	_, err = source.Load(context.Background())
	if err == nil || strings.Contains(err.Error(), "canary-secret-value") {
		t.Fatalf("Source.Load() error = %v, want redacted parse error", err)
	}
}

func TestOversizedIntegerDoesNotExposeSensitiveToken(t *testing.T) {
	t.Parallel()

	const canary = "12345678901234567890123456789012345678901234567890"
	source, err := yamlsource.Bytes(
		[]byte("token: !!int "+canary+"\n"),
		yamlsource.Options{Name: "secret-yaml", Sensitive: true},
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
		limits yamlsource.Limits
	}{
		"bytes": {data: "long: value\n", limits: yamlsource.Limits{MaxBytes: 4}},
		"depth": {data: "one:\n  two:\n    three: true\n", limits: yamlsource.Limits{MaxDepth: 2}},
		"keys":  {data: "one: 1\ntwo: 2\n", limits: yamlsource.Limits{MaxKeys: 1}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source, err := yamlsource.Bytes(
				[]byte(test.data), yamlsource.Options{Name: "yaml", Limits: test.limits},
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

	missing, err := yamlsource.FromFS(
		fstest.MapFS{}, "missing.yaml", yamlsource.Options{Name: "yaml", Optional: true},
	)
	if err != nil {
		t.Fatalf("FromFS() error = %v", err)
	}
	_, err = missing.Load(context.Background())
	if !errors.Is(err, config.ErrNotFound) {
		t.Fatalf("Source.Load() error = %v, want ErrNotFound", err)
	}

	malformed, err := yamlsource.FromFS(
		fstest.MapFS{"broken.yaml": &fstest.MapFile{Data: []byte("token: [")}},
		"broken.yaml", yamlsource.Options{Name: "yaml", Optional: true},
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

	source, err := yamlsource.FromFS(permissionFS{}, "secret.yaml", yamlsource.Options{Name: "yaml"})
	if err != nil {
		t.Fatalf("FromFS() error = %v", err)
	}
	_, err = source.Load(context.Background())
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("Source.Load() error = %v, want fs.ErrPermission", err)
	}

	bytesSource, err := yamlsource.Bytes(nil, yamlsource.Options{Name: "yaml"})
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
