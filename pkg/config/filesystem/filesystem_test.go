package filesystem_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"

	config "github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/discover"
	"github.com/faustbrian/golib/pkg/config/filesystem"
)

func TestFromFSDispatchesSupportedExtensions(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		path string
		data string
		want map[string]any
	}{
		"json": {path: "config.json", data: `{"name":"json"}`, want: map[string]any{"name": "json"}},
		"yaml": {path: "config.yaml", data: "name: yaml\n", want: map[string]any{"name": "yaml"}},
		"yml":  {path: "config.yml", data: "name: yml\n", want: map[string]any{"name": "yml"}},
		"toml": {path: "config.toml", data: `name = "toml"`, want: map[string]any{"name": "toml"}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source, err := filesystem.FromFS(
				fstest.MapFS{test.path: &fstest.MapFile{Data: []byte(test.data)}},
				test.path,
				filesystem.Options{Name: name},
			)
			if err != nil {
				t.Fatalf("FromFS() error = %v", err)
			}
			document, err := source.Load(context.Background())
			if err != nil {
				t.Fatalf("Source.Load() error = %v", err)
			}
			if !reflect.DeepEqual(document.Tree, test.want) {
				t.Fatalf("Source.Load() tree = %#v, want %#v", document.Tree, test.want)
			}
		})
	}
}

func TestFromFSRejectsUnsupportedFormatUnlessExplicit(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{"config.data": &fstest.MapFile{Data: []byte(`{"name":"json"}`)}}
	if _, err := filesystem.FromFS(files, "config.data", filesystem.Options{Name: "auto"}); err == nil {
		t.Fatal("FromFS() error = nil, want unsupported extension")
	}
	source, err := filesystem.FromFS(files, "config.data", filesystem.Options{
		Name: "explicit", Format: filesystem.FormatJSON,
	})
	if err != nil {
		t.Fatalf("FromFS() error = %v", err)
	}
	if _, err := source.Load(context.Background()); err != nil {
		t.Fatalf("Source.Load() error = %v", err)
	}
}

func TestFromPathReloadsAtomicallyReplacedFile(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	mustWrite(t, path, `{"name":"first"}`)
	source, err := filesystem.FromPath(path, filesystem.Options{Name: "file"})
	if err != nil {
		t.Fatalf("FromPath() error = %v", err)
	}
	first, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Source.Load() error = %v", err)
	}
	if first.Tree["name"] != "first" {
		t.Fatalf("first name = %#v", first.Tree["name"])
	}

	replacement := filepath.Join(directory, "replacement.json")
	mustWrite(t, replacement, `{"name":"second"}`)
	if err := os.Rename(replacement, path); err != nil {
		t.Fatalf("Rename() error = %v", err)
	}
	second, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Source.Load() error = %v", err)
	}
	if second.Tree["name"] != "second" {
		t.Fatalf("second name = %#v", second.Tree["name"])
	}
}

func TestFromDiscoveredPreservesPathProvenance(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "config.yaml")
	mustWrite(t, path, "nested:\n  name: api\n")
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks() error = %v", err)
	}
	source, err := filesystem.FromDiscovered(discover.Result{
		Path: path, ResolvedPath: resolved, Directory: directory, SearchPlace: "config.yaml",
	}, filesystem.Options{Name: "discovered"})
	if err != nil {
		t.Fatalf("FromDiscovered() error = %v", err)
	}
	plan, err := config.NewPlan(source)
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	snapshot, err := config.LoadTree(context.Background(), plan)
	if err != nil {
		t.Fatalf("LoadTree() error = %v", err)
	}
	for _, field := range []string{"nested", "nested.name"} {
		origin, ok := snapshot.Origin(field)
		if !ok || origin.Location != path {
			t.Fatalf("Origin(%q) = %#v, %v", field, origin, ok)
		}
	}
}

func TestOptionalFileSuppressesOnlyAbsence(t *testing.T) {
	t.Parallel()

	missing, err := filesystem.FromFS(
		fstest.MapFS{}, "missing.json", filesystem.Options{Name: "optional", Optional: true},
	)
	if err != nil {
		t.Fatalf("FromFS() error = %v", err)
	}
	_, err = missing.Load(context.Background())
	if !errors.Is(err, config.ErrNotFound) {
		t.Fatalf("Source.Load() error = %v, want ErrNotFound", err)
	}

	broken, err := filesystem.FromFS(
		fstest.MapFS{"broken.json": &fstest.MapFile{Data: []byte(`{"broken":`)}},
		"broken.json", filesystem.Options{Name: "optional", Optional: true},
	)
	if err != nil {
		t.Fatalf("FromFS() error = %v", err)
	}
	_, err = broken.Load(context.Background())
	if err == nil || errors.Is(err, config.ErrNotFound) {
		t.Fatalf("Source.Load() error = %v, want parse error", err)
	}
}

func TestFromFSValidatesOptions(t *testing.T) {
	t.Parallel()

	if _, err := filesystem.FromFS(fstest.MapFS{}, "config.json", filesystem.Options{}); err == nil {
		t.Fatal("FromFS() error = nil, want missing name")
	}
	if _, err := filesystem.FromFS(nil, "config.json", filesystem.Options{Name: "file"}); err == nil {
		t.Fatal("FromFS() error = nil, want nil filesystem")
	}
}

func TestReaderOpensFreshContextAwareInputForEveryLoad(t *testing.T) {
	t.Parallel()

	loads := 0
	source, err := filesystem.Reader(func(ctx context.Context) (io.ReadCloser, error) {
		if ctx == nil {
			t.Fatal("Reader() opener context = nil")
		}
		loads++
		value := `{"name":"first"}`
		if loads == 2 {
			value = `{"name":"second"}`
		}
		return io.NopCloser(bytes.NewBufferString(value)), nil
	}, filesystem.Options{Name: "reader", Format: filesystem.FormatJSON})
	if err != nil {
		t.Fatalf("Reader() error = %v", err)
	}
	first, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Source.Load() error = %v", err)
	}
	second, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Source.Load() error = %v", err)
	}
	if first.Tree["name"] != "first" || second.Tree["name"] != "second" || loads != 2 {
		t.Fatalf("reader loads = %#v, %#v, %d", first.Tree, second.Tree, loads)
	}
}

func TestReaderEnforcesReadAndLifecycleFailures(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		open filesystem.OpenFunc
		want error
	}{
		"open": {
			open: func(context.Context) (io.ReadCloser, error) { return nil, fs.ErrPermission },
			want: fs.ErrPermission,
		},
		"partial read": {
			open: func(context.Context) (io.ReadCloser, error) {
				return &readCloser{Reader: io.MultiReader(
					bytes.NewBufferString(`{"name":`), errorReader{err: io.ErrUnexpectedEOF},
				)}, nil
			},
			want: io.ErrUnexpectedEOF,
		},
		"close": {
			open: func(context.Context) (io.ReadCloser, error) {
				return &readCloser{Reader: bytes.NewBufferString(`{"name":"api"}`), closeErr: fs.ErrClosed}, nil
			},
			want: fs.ErrClosed,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source, err := filesystem.Reader(test.open, filesystem.Options{
				Name: "reader", Format: filesystem.FormatJSON,
			})
			if err != nil {
				t.Fatalf("Reader() error = %v", err)
			}
			if _, err := source.Load(context.Background()); !errors.Is(err, test.want) {
				t.Fatalf("Source.Load() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestReaderHonorsCancellationAndByteLimit(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	reader := &cancelingReader{cancel: cancel, remaining: []byte(`{"name":"api"}`)}
	source, err := filesystem.Reader(func(context.Context) (io.ReadCloser, error) {
		return &readCloser{Reader: reader}, nil
	}, filesystem.Options{Name: "reader", Format: filesystem.FormatJSON})
	if err != nil {
		t.Fatalf("Reader() error = %v", err)
	}
	if _, err := source.Load(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Source.Load() error = %v, want context.Canceled", err)
	}

	limited, err := filesystem.Reader(func(context.Context) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewBufferString(`{"name":"api"}`)), nil
	}, filesystem.Options{
		Name: "reader", Format: filesystem.FormatJSON,
		Limits: filesystem.Limits{MaxBytes: 4},
	})
	if err != nil {
		t.Fatalf("Reader() error = %v", err)
	}
	if _, err := limited.Load(context.Background()); err == nil {
		t.Fatal("Source.Load() error = nil, want byte limit")
	}
}

func TestReaderRequiresFactoryAndExplicitFormat(t *testing.T) {
	t.Parallel()

	if _, err := filesystem.Reader(nil, filesystem.Options{Name: "reader", Format: filesystem.FormatJSON}); err == nil {
		t.Fatal("Reader() error = nil, want nil factory error")
	}
	if _, err := filesystem.Reader(func(context.Context) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(nil)), nil
	}, filesystem.Options{Name: "reader"}); err == nil {
		t.Fatal("Reader() error = nil, want explicit format error")
	}
}

type readCloser struct {
	io.Reader
	closeErr error
}

func (r *readCloser) Close() error { return r.closeErr }

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

type cancelingReader struct {
	cancel    context.CancelFunc
	remaining []byte
}

func (r *cancelingReader) Read(buffer []byte) (int, error) {
	if len(r.remaining) == 0 {
		return 0, io.EOF
	}
	buffer[0] = r.remaining[0]
	r.remaining = r.remaining[1:]
	r.cancel()
	return 1, nil
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
