package toml

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"time"

	config "github.com/faustbrian/golib/pkg/config"
)

func TestParseErrorHasStableSecretSafeFormatting(t *testing.T) {
	t.Parallel()

	cause := errors.New("canary-secret")
	err := &ParseError{Cause: cause}
	if got := err.Error(); got != "decode TOML config: malformed document" {
		t.Fatalf("ParseError.Error() = %q", got)
	}
	if !errors.Is(err, cause) {
		t.Fatal("ParseError does not unwrap its cause")
	}
	if unwrapped := errors.Unwrap(err); unwrapped == nil ||
		strings.Contains(unwrapped.Error(), "canary-secret") {
		t.Fatalf("ParseError.Unwrap() leaked cause: %v", unwrapped)
	}
	if got := fmt.Sprintf("%#v", err); got != err.Error() {
		t.Fatalf("formatted ParseError = %q", got)
	}
	if text, marshalErr := err.MarshalText(); marshalErr != nil || string(text) != err.Error() {
		t.Fatalf("ParseError.MarshalText() = %q, %v", text, marshalErr)
	}
}

func TestConstructorsValidateInputsAndPreserveImmutableMetadata(t *testing.T) {
	t.Parallel()

	for name, options := range map[string]Options{
		"empty name":     {},
		"negative bytes": {Name: "toml", Limits: Limits{MaxBytes: -1}},
		"negative depth": {Name: "toml", Limits: Limits{MaxDepth: -1}},
		"negative keys":  {Name: "toml", Limits: Limits{MaxKeys: -1}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := Bytes(nil, options); err == nil {
				t.Fatal("Bytes() error = nil")
			}
		})
	}
	if _, err := FromFS(nil, "config.toml", Options{Name: "toml"}); err == nil {
		t.Fatal("FromFS(nil) error = nil")
	}
	if _, err := FromFS(emptyTOMLFS{}, "../config.toml", Options{Name: "toml"}); err == nil {
		t.Fatal("FromFS(invalid path) error = nil")
	}
	if _, err := FromFS(emptyTOMLFS{}, "config.toml", Options{}); err == nil {
		t.Fatal("FromFS(invalid options) error = nil")
	}

	data := []byte(`value = "original"`)
	source, err := Bytes(data, Options{
		Name: "toml", Priority: 42, Sensitive: true, Optional: true,
	})
	if err != nil {
		t.Fatalf("Bytes() error = %v", err)
	}
	data[9] = 'X'
	if got := source.Info(); got != (config.SourceInfo{
		Name: "toml", Priority: 42, Sensitive: true, Optional: true,
	}) {
		t.Fatalf("Info() = %#v", got)
	}
	document, err := source.Load(context.Background())
	if err != nil || document.Tree["value"] != "original" {
		t.Fatalf("Load() = %#v, %v", document, err)
	}
}

func TestFSSourcePropagatesReadCloseAndLimitErrors(t *testing.T) {
	t.Parallel()

	readFailure := errors.New("read failure")
	closeFailure := errors.New("close failure")
	tests := map[string]struct {
		filesystem fs.FS
		limits     Limits
		want       error
	}{
		"read": {
			filesystem: openTOMLFS(func(string) (fs.File, error) {
				return &scriptedTOMLFile{reader: errorTOMLReader{err: readFailure}}, nil
			}),
			want: readFailure,
		},
		"close": {
			filesystem: openTOMLFS(func(string) (fs.File, error) {
				return &scriptedTOMLFile{reader: strings.NewReader("value = true"), closeErr: closeFailure}, nil
			}),
			want: closeFailure,
		},
		"limit": {
			filesystem: openTOMLFS(func(string) (fs.File, error) {
				return &scriptedTOMLFile{reader: strings.NewReader(`long = "value"`)}, nil
			}),
			limits: Limits{MaxBytes: 4},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source, err := FromFS(test.filesystem, "config.toml", Options{
				Name: "toml", Limits: test.limits,
			})
			if err != nil {
				t.Fatalf("FromFS() error = %v", err)
			}
			_, err = source.Load(context.Background())
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("Load() error = %v, want %v", err, test.want)
			}
			if test.want == nil && err == nil {
				t.Fatal("Load() error = nil")
			}
		})
	}
}

func TestSourceNormalizesEveryTOMLDateTimeCategory(t *testing.T) {
	t.Parallel()

	source, err := Bytes([]byte(strings.Join([]string{
		"date = 1979-05-27",
		"time = 07:32:00.123",
		"local = 1979-05-27T07:32:00",
		"offset = 1979-05-27T07:32:00Z",
	}, "\n")), Options{Name: "toml"})
	if err != nil {
		t.Fatalf("Bytes() error = %v", err)
	}
	document, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := map[string]any{
		"date": "1979-05-27", "time": "07:32:00.123",
		"local": "1979-05-27T07:32:00", "offset": "1979-05-27T07:32:00Z",
	}
	if !reflect.DeepEqual(document.Tree, want) {
		t.Fatalf("Load() tree = %#v, want %#v", document.Tree, want)
	}
}

func TestNormalizeCollectionAndUnsupportedValueBoundaries(t *testing.T) {
	t.Parallel()

	limits := Limits{MaxDepth: 10, MaxKeys: 10}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	keys := 0
	if _, err := normalize(canceled, map[string]any{}, 1, "", limits, &keys); !errors.Is(err, context.Canceled) {
		t.Fatalf("normalize(canceled) error = %v", err)
	}
	tests := map[string]struct {
		value any
		want  any
	}{
		"nil": {value: nil, want: nil},
		"array": {
			value: []any{"value", true, int64(1), 1.5},
			want:  []any{"value", true, int64(1), 1.5},
		},
		"table array": {
			value: []map[string]any{{"value": int64(1)}},
			want:  []any{map[string]any{"value": int64(1)}},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			keys := 0
			got, err := normalize(context.Background(), test.value, 1, "root", limits, &keys)
			if err != nil || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("normalize() = %#v, %v; want %#v", got, err, test.want)
			}
		})
	}

	for name, value := range map[string]any{
		"unsupported":       make(chan struct{}),
		"array child":       []any{make(chan struct{})},
		"table array child": []map[string]any{{"bad": make(chan struct{})}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			keys := 0
			if _, err := normalize(context.Background(), value, 1, "root", limits, &keys); err == nil {
				t.Fatal("normalize() error = nil")
			}
		})
	}
}

func TestSourceHonorsCancellationDuringNormalization(t *testing.T) {
	t.Parallel()

	source, err := Bytes([]byte("[outer]\ninner = true\n"), Options{Name: "toml"})
	if err != nil {
		t.Fatalf("Bytes() error = %v", err)
	}
	_, err = source.Load(&stagedTOMLContext{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Load() error = %v, want context.Canceled", err)
	}
}

type stagedTOMLContext struct{ calls int }

func (*stagedTOMLContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*stagedTOMLContext) Done() <-chan struct{}       { return nil }
func (*stagedTOMLContext) Value(any) any               { return nil }
func (c *stagedTOMLContext) Err() error {
	c.calls++
	if c.calls > 1 {
		return context.Canceled
	}
	return nil
}

type emptyTOMLFS struct{}

func (emptyTOMLFS) Open(string) (fs.File, error) { return nil, fs.ErrNotExist }

type openTOMLFS func(string) (fs.File, error)

func (f openTOMLFS) Open(name string) (fs.File, error) { return f(name) }

type scriptedTOMLFile struct {
	reader   io.Reader
	closeErr error
}

func (f *scriptedTOMLFile) Read(buffer []byte) (int, error) { return f.reader.Read(buffer) }
func (f *scriptedTOMLFile) Close() error                    { return f.closeErr }
func (*scriptedTOMLFile) Stat() (fs.FileInfo, error)        { return tomlFileInfo{}, nil }

type errorTOMLReader struct{ err error }

func (r errorTOMLReader) Read([]byte) (int, error) { return 0, r.err }

type tomlFileInfo struct{}

func (tomlFileInfo) Name() string       { return "config.toml" }
func (tomlFileInfo) Size() int64        { return 0 }
func (tomlFileInfo) Mode() fs.FileMode  { return 0o600 }
func (tomlFileInfo) ModTime() time.Time { return time.Time{} }
func (tomlFileInfo) IsDir() bool        { return false }
func (tomlFileInfo) Sys() any           { return nil }
