package config_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"reflect"
	"testing"
	"time"

	config "github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/dotenv"
	jsonsource "github.com/faustbrian/golib/pkg/config/json"
	tomlsource "github.com/faustbrian/golib/pkg/config/toml"
	yamlsource "github.com/faustbrian/golib/pkg/config/yaml"
)

func TestJSONYAMLAndTOMLEquivalentDocumentsProduceSameTree(t *testing.T) {
	t.Parallel()

	sources := map[string]sourceLoader{
		"json": func() (map[string]any, error) {
			source, err := jsonsource.Bytes(
				[]byte(`{"name":"api","port":8080,"enabled":true,"items":["one","two"],"nested":{"host":"localhost"}}`),
				jsonsource.Options{Name: "json"},
			)
			if err != nil {
				return nil, err
			}
			document, err := source.Load(context.Background())
			return document.Tree, err
		},
		"yaml": func() (map[string]any, error) {
			source, err := yamlsource.Bytes(
				[]byte("name: api\nport: 8080\nenabled: true\nitems: [one, two]\nnested:\n  host: localhost\n"),
				yamlsource.Options{Name: "yaml"},
			)
			if err != nil {
				return nil, err
			}
			document, err := source.Load(context.Background())
			return document.Tree, err
		},
		"toml": func() (map[string]any, error) {
			source, err := tomlsource.Bytes(
				[]byte("name = \"api\"\nport = 8080\nenabled = true\nitems = [\"one\", \"two\"]\n[nested]\nhost = \"localhost\"\n"),
				tomlsource.Options{Name: "toml"},
			)
			if err != nil {
				return nil, err
			}
			document, err := source.Load(context.Background())
			return document.Tree, err
		},
	}

	var reference map[string]any
	for name, load := range sources {
		tree, err := load()
		if err != nil {
			t.Fatalf("%s load error = %v", name, err)
		}
		if reference == nil {
			reference = tree
			continue
		}
		if !reflect.DeepEqual(tree, reference) {
			t.Fatalf("%s tree = %#v, want %#v", name, tree, reference)
		}
	}
}

func TestJSONYAMLAndTOMLArrayOfObjectsProduceSameTree(t *testing.T) {
	t.Parallel()

	sources := map[string]sourceLoader{
		"json": structuredLoader(t, "json", []byte(`{"servers":[{"host":"one","port":80},{"host":"two","port":443}]}`)),
		"yaml": structuredLoader(t, "yaml", []byte("servers:\n  - host: one\n    port: 80\n  - host: two\n    port: 443\n")),
		"toml": structuredLoader(t, "toml", []byte("[[servers]]\nhost = \"one\"\nport = 80\n[[servers]]\nhost = \"two\"\nport = 443\n")),
	}

	want := map[string]any{
		"servers": []any{
			map[string]any{"host": "one", "port": int64(80)},
			map[string]any{"host": "two", "port": int64(443)},
		},
	}
	for name, load := range sources {
		tree, err := load()
		if err != nil {
			t.Fatalf("%s load error = %v", name, err)
		}
		if !reflect.DeepEqual(tree, want) {
			t.Fatalf("%s tree = %#v, want %#v", name, tree, want)
		}
	}
}

func TestCrossFormatDifferencesAreIntentionalAndNormalized(t *testing.T) {
	t.Parallel()

	jsonTree, err := structuredLoader(
		t,
		"json",
		[]byte(`{"nothing":null,"timestamp":"1979-05-27T07:32:00Z"}`),
	)()
	if err != nil {
		t.Fatalf("JSON load error = %v", err)
	}
	yamlTree, err := structuredLoader(
		t,
		"yaml",
		[]byte("nothing: null\ntimestamp: 1979-05-27T07:32:00Z\n"),
	)()
	if err != nil {
		t.Fatalf("YAML load error = %v", err)
	}
	tomlTree, err := structuredLoader(
		t,
		"toml",
		[]byte("timestamp = 1979-05-27T07:32:00Z\n"),
	)()
	if err != nil {
		t.Fatalf("TOML load error = %v", err)
	}

	if jsonTree["nothing"] != nil || yamlTree["nothing"] != nil {
		t.Fatalf("JSON/YAML nulls = %#v / %#v, want nil", jsonTree["nothing"], yamlTree["nothing"])
	}
	if _, exists := tomlTree["nothing"]; exists {
		t.Fatalf("TOML unexpectedly represented null: %#v", tomlTree)
	}
	for name, tree := range map[string]map[string]any{
		"json": jsonTree,
		"yaml": yamlTree,
		"toml": tomlTree,
	} {
		if got := tree["timestamp"]; got != "1979-05-27T07:32:00Z" {
			t.Fatalf("%s timestamp = %#v, want normalized string", name, got)
		}
	}
}

func TestFilesystemBackedFormatsRejectMutationDuringRead(t *testing.T) {
	t.Parallel()

	type environmentSettings struct {
		Value string `config:"value" env:"VALUE"`
	}
	constructors := map[string]func(fs.FS) (config.Source, error){
		"json": func(filesystem fs.FS) (config.Source, error) {
			return jsonsource.FromFS(filesystem, "config.json", jsonsource.Options{Name: "json"})
		},
		"yaml": func(filesystem fs.FS) (config.Source, error) {
			return yamlsource.FromFS(filesystem, "config.yaml", yamlsource.Options{Name: "yaml"})
		},
		"toml": func(filesystem fs.FS) (config.Source, error) {
			return tomlsource.FromFS(filesystem, "config.toml", tomlsource.Options{Name: "toml"})
		},
		"dotenv": func(filesystem fs.FS) (config.Source, error) {
			return dotenv.FromFSFor[environmentSettings](
				filesystem,
				"config.env",
				dotenv.Options{Name: "dotenv"},
			)
		},
	}

	for name, construct := range constructors {
		name := name
		construct := construct
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source, err := construct(changingFormatFS{})
			if err != nil {
				t.Fatalf("constructor error = %v", err)
			}
			if _, err := source.Load(context.Background()); !errors.Is(err, config.ErrSourceChanged) {
				t.Fatalf("Load() error = %v, want ErrSourceChanged", err)
			}
		})
	}
}

func structuredLoader(t *testing.T, format string, data []byte) sourceLoader {
	t.Helper()
	return func() (map[string]any, error) {
		var (
			source interface {
				Load(context.Context) (config.Document, error)
			}
			err error
		)
		switch format {
		case "json":
			source, err = jsonsource.Bytes(data, jsonsource.Options{Name: "json"})
		case "yaml":
			source, err = yamlsource.Bytes(data, yamlsource.Options{Name: "yaml"})
		case "toml":
			source, err = tomlsource.Bytes(data, tomlsource.Options{Name: "toml"})
		default:
			t.Fatalf("unsupported test format %q", format)
		}
		if err != nil {
			return nil, err
		}
		document, err := source.Load(context.Background())
		return document.Tree, err
	}
}

type sourceLoader func() (map[string]any, error)

type changingFormatFS struct{}

func (changingFormatFS) Open(string) (fs.File, error) {
	return &changingFormatFile{Reader: bytes.NewReader([]byte("{}"))}, nil
}

type changingFormatFile struct {
	*bytes.Reader
	stats int
}

func (f *changingFormatFile) Close() error { return nil }

func (f *changingFormatFile) Stat() (fs.FileInfo, error) {
	f.stats++
	return changingFormatFileInfo{size: int64(4 - f.stats)}, nil
}

type changingFormatFileInfo struct{ size int64 }

func (changingFormatFileInfo) Name() string       { return "config" }
func (info changingFormatFileInfo) Size() int64   { return info.size }
func (changingFormatFileInfo) Mode() fs.FileMode  { return 0o600 }
func (changingFormatFileInfo) ModTime() time.Time { return time.Unix(0, 0) }
func (changingFormatFileInfo) IsDir() bool        { return false }
func (changingFormatFileInfo) Sys() any           { return nil }
