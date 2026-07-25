package json_test

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	config "github.com/faustbrian/golib/pkg/config"
	jsonsource "github.com/faustbrian/golib/pkg/config/json"
)

func TestFromFSValidatesFilesystemPathAndOptions(t *testing.T) {
	t.Parallel()

	if _, err := jsonsource.FromFS(nil, "config.json", jsonsource.Options{Name: "json"}); err == nil {
		t.Fatal("FromFS(nil) error = nil")
	}
	if _, err := jsonsource.FromFS(emptyFS{}, "../config.json", jsonsource.Options{Name: "json"}); err == nil {
		t.Fatal("FromFS(invalid path) error = nil")
	}
	if _, err := jsonsource.FromFS(emptyFS{}, "config.json", jsonsource.Options{}); err == nil {
		t.Fatal("FromFS(invalid options) error = nil")
	}
}

func TestBytesAreImmutableAndPreserveSourceMetadata(t *testing.T) {
	t.Parallel()

	data := []byte(`{"value":"original"}`)
	source, err := jsonsource.Bytes(data, jsonsource.Options{
		Name: "json", Priority: 42, Sensitive: true, Optional: true,
	})
	if err != nil {
		t.Fatalf("Bytes() error = %v", err)
	}
	data[10] = 'X'
	if got := source.Info(); got != (config.SourceInfo{
		Name: "json", Priority: 42, Sensitive: true, Optional: true,
	}) {
		t.Fatalf("Info() = %#v", got)
	}
	document, err := source.Load(context.Background())
	if err != nil || document.Tree["value"] != "original" {
		t.Fatalf("Load() = %#v, %v", document, err)
	}
}

func TestFSSourcePropagatesReadCloseLimitAndCancellationErrors(t *testing.T) {
	t.Parallel()

	readFailure := errors.New("read failure")
	closeFailure := errors.New("close failure")
	tests := map[string]struct {
		filesystem fs.FS
		limits     jsonsource.Limits
		ctx        context.Context
		want       error
	}{
		"read": {
			filesystem: openFS(func(string) (fs.File, error) {
				return &scriptedFile{reader: errorReader{err: readFailure}}, nil
			}),
			want: readFailure,
		},
		"close": {
			filesystem: openFS(func(string) (fs.File, error) {
				return &scriptedFile{reader: strings.NewReader(`{}`), closeErr: closeFailure}, nil
			}),
			want: closeFailure,
		},
		"limit": {
			filesystem: openFS(func(string) (fs.File, error) {
				return &scriptedFile{reader: strings.NewReader(`{"long":"value"}`)}, nil
			}),
			limits: jsonsource.Limits{MaxBytes: 4},
		},
		"cancellation": {
			filesystem: openFS(func(string) (fs.File, error) {
				return &scriptedFile{reader: strings.NewReader(`{}`)}, nil
			}),
			ctx:  &stagedJSONContext{},
			want: context.Canceled,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source, err := jsonsource.FromFS(test.filesystem, "config.json", jsonsource.Options{
				Name: "json", Limits: test.limits,
			})
			if err != nil {
				t.Fatalf("FromFS() error = %v", err)
			}
			ctx := test.ctx
			if ctx == nil {
				ctx = context.Background()
			}
			_, err = source.Load(ctx)
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("Load() error = %v, want %v", err, test.want)
			}
			if test.want == nil && err == nil {
				t.Fatal("Load() error = nil")
			}
		})
	}
}

func TestSourceRejectsEveryInvalidRootAndDelimiterState(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"empty":               "",
		"array root":          `[]`,
		"null root":           `null`,
		"multiple roots":      `{} {}`,
		"trailing malformed":  `{} trailing`,
		"invalid object key":  `{bad:1}`,
		"truncated object":    `{`,
		"truncated array":     `{"items":[`,
		"nested value":        `{"value":}`,
		"invalid array value": `{"items":[tru]}`,
		"nested array value":  `{"items":[]`,
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source, err := jsonsource.Bytes([]byte(data), jsonsource.Options{Name: "json"})
			if err != nil {
				t.Fatalf("Bytes() error = %v", err)
			}
			if _, err := source.Load(context.Background()); err == nil {
				t.Fatal("Load() error = nil")
			}
		})
	}
}

func TestSourceConvertsNullNegativeAndUnsignedNumberBoundaries(t *testing.T) {
	t.Parallel()

	source, err := jsonsource.Bytes([]byte(
		`{"null":null,"negative":-1,"unsigned":18446744073709551615,"exponent":1e2}`,
	), jsonsource.Options{Name: "json"})
	if err != nil {
		t.Fatalf("Bytes() error = %v", err)
	}
	document, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := map[string]any{
		"null": nil, "negative": int64(-1), "unsigned": uint64(math.MaxUint64), "exponent": float64(100),
	}
	if !reflect.DeepEqual(document.Tree, want) {
		t.Fatalf("Load() tree = %#v, want %#v", document.Tree, want)
	}
}

func TestSourceRejectsNumberOverflowCategories(t *testing.T) {
	t.Parallel()

	for name, data := range map[string]string{
		"float":   `{"value":1e10000}`,
		"integer": `{"value":18446744073709551616}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source, err := jsonsource.Bytes([]byte(data), jsonsource.Options{Name: "json"})
			if err != nil {
				t.Fatalf("Bytes() error = %v", err)
			}
			if _, err := source.Load(context.Background()); err == nil {
				t.Fatal("Load() error = nil")
			}
		})
	}
}

func TestDuplicateKeyErrorFormattingIsStable(t *testing.T) {
	t.Parallel()

	err := (&jsonsource.DuplicateKeyError{Path: "server.port"}).Error()
	if err != `decode JSON config: duplicate key at "server.port"` {
		t.Fatalf("DuplicateKeyError.Error() = %q", err)
	}
}

func TestSourceHonorsCancellationDuringParsing(t *testing.T) {
	t.Parallel()

	source, err := jsonsource.Bytes([]byte(`{"value":true}`), jsonsource.Options{Name: "json"})
	if err != nil {
		t.Fatalf("Bytes() error = %v", err)
	}
	_, err = source.Load(&stagedJSONContext{allow: 1})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Load() error = %v, want context.Canceled", err)
	}
}

type emptyFS struct{}

func (emptyFS) Open(string) (fs.File, error) { return nil, fs.ErrNotExist }

type openFS func(string) (fs.File, error)

func (f openFS) Open(name string) (fs.File, error) { return f(name) }

type scriptedFile struct {
	reader   io.Reader
	closeErr error
}

func (f *scriptedFile) Read(buffer []byte) (int, error) { return f.reader.Read(buffer) }
func (f *scriptedFile) Close() error                    { return f.closeErr }
func (*scriptedFile) Stat() (fs.FileInfo, error)        { return fileInfo{}, nil }

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

type fileInfo struct{}

func (fileInfo) Name() string       { return "config.json" }
func (fileInfo) Size() int64        { return 0 }
func (fileInfo) Mode() fs.FileMode  { return 0o600 }
func (fileInfo) ModTime() time.Time { return time.Time{} }
func (fileInfo) IsDir() bool        { return false }
func (fileInfo) Sys() any           { return nil }

type stagedJSONContext struct {
	calls int
	allow int
}

func (c *stagedJSONContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *stagedJSONContext) Done() <-chan struct{}       { return nil }
func (c *stagedJSONContext) Value(any) any               { return nil }
func (c *stagedJSONContext) Err() error {
	c.calls++
	if c.calls > c.allow+1 {
		return context.Canceled
	}
	return nil
}
