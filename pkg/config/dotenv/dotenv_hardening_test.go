package dotenv

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"time"

	config "github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/internal/sourceio"
)

type hardeningConfiguration struct {
	Value string `config:"value" env:"VALUE"`
}

func TestErrorsHaveStableSecretSafeFormatting(t *testing.T) {
	t.Parallel()

	if got := (&DuplicateKeyError{Name: "VALUE", FirstLine: 1, Line: 2}).Error(); got != `decode dotenv: duplicate variable "VALUE" at line 2 (first at line 1)` {
		t.Fatalf("DuplicateKeyError.Error() = %q", got)
	}
	if got := (&SyntaxError{Line: 2, Column: 3, Reason: "bad syntax"}).Error(); got != "decode dotenv at 2:3: bad syntax" {
		t.Fatalf("SyntaxError.Error() = %q", got)
	}
	if got := (&InterpolationError{Name: "VALUE", Reason: "cycle detected"}).Error(); got != `interpolate dotenv variable "VALUE": cycle detected` {
		t.Fatalf("InterpolationError.Error() = %q", got)
	}
}

func TestConstructorsValidateInputsAndPreserveImmutableOptions(t *testing.T) {
	t.Parallel()

	for name, options := range map[string]Options{
		"empty name": {},
		"negative source limit": {
			Name: "dotenv", Limits: Limits{MaxLines: -1},
		},
		"negative interpolation limit": {
			Name: "dotenv", Interpolation: &Interpolation{MaxDepth: -1},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := BytesFor[hardeningConfiguration](nil, options); err == nil {
				t.Fatal("BytesFor() error = nil")
			}
		})
	}
	if _, err := FromFSFor[hardeningConfiguration](nil, "config.env", Options{Name: "dotenv"}); err == nil {
		t.Fatal("FromFSFor(nil) error = nil")
	}
	if _, err := FromFSFor[hardeningConfiguration](emptyDotenvFS{}, "../config.env", Options{Name: "dotenv"}); err == nil {
		t.Fatal("FromFSFor(invalid path) error = nil")
	}
	if _, err := FromFSFor[hardeningConfiguration](emptyDotenvFS{}, "config.env", Options{}); err == nil {
		t.Fatal("FromFSFor(invalid options) error = nil")
	}
	if _, err := BytesFor[int](nil, Options{Name: "dotenv"}); err == nil {
		t.Fatal("BytesFor(non-struct) error = nil")
	}

	data := []byte("VALUE=original")
	variables := map[string]string{"OTHER": "original"}
	source, err := BytesFor[hardeningConfiguration](data, Options{
		Name: "dotenv", Priority: 42, Sensitive: true, Optional: true,
		Interpolation: &Interpolation{Variables: variables},
	})
	if err != nil {
		t.Fatalf("BytesFor() error = %v", err)
	}
	data[6] = 'X'
	variables["OTHER"] = "changed"
	if got := source.Info(); got != (config.SourceInfo{
		Name: "dotenv", Priority: 42, Sensitive: true, Optional: true,
	}) {
		t.Fatalf("Info() = %#v", got)
	}
	document, err := source.Load(context.Background())
	if err != nil || document.Tree["value"] != "original" {
		t.Fatalf("Load() = %#v, %v", document, err)
	}
}

func TestFSSourcePropagatesOpenReadCloseLimitAndCancellation(t *testing.T) {
	t.Parallel()

	openFailure := errors.New("open failure")
	readFailure := errors.New("read failure")
	closeFailure := errors.New("close failure")
	tests := map[string]struct {
		filesystem fs.FS
		limits     Limits
		ctx        context.Context
		want       error
	}{
		"open": {filesystem: openDotenvFS(func(string) (fs.File, error) {
			return nil, openFailure
		}), want: openFailure},
		"read": {filesystem: openDotenvFS(func(string) (fs.File, error) {
			return &scriptedDotenvFile{reader: errorDotenvReader{err: readFailure}}, nil
		}), want: readFailure},
		"close": {filesystem: openDotenvFS(func(string) (fs.File, error) {
			return &scriptedDotenvFile{reader: strings.NewReader("VALUE=ok"), closeErr: closeFailure}, nil
		}), want: closeFailure},
		"limit": {filesystem: openDotenvFS(func(string) (fs.File, error) {
			return &scriptedDotenvFile{reader: strings.NewReader("VALUE=too-long")}, nil
		}), limits: Limits{MaxBytes: 4}},
		"cancellation": {filesystem: openDotenvFS(func(string) (fs.File, error) {
			return &scriptedDotenvFile{reader: strings.NewReader("VALUE=ok")}, nil
		}), ctx: &stagedDotenvContext{}, want: context.Canceled},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source, err := FromFSFor[hardeningConfiguration](
				test.filesystem,
				"config.env",
				Options{Name: "dotenv", Limits: test.limits},
			)
			if err != nil {
				t.Fatalf("FromFSFor() error = %v", err)
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

func TestParseRejectsEverySyntaxAndLimitCategory(t *testing.T) {
	t.Parallel()

	limits, err := normalizeLimits(Limits{})
	if err != nil {
		t.Fatalf("normalizeLimits() error = %v", err)
	}
	tests := map[string]struct {
		data   string
		limits Limits
	}{
		"nul":                      {data: "VALUE=bad\x00value", limits: limits},
		"line limit":               {data: "A=1\nB=2", limits: Limits{MaxLines: 1, MaxLineBytes: 100, MaxKeys: 100}},
		"line byte limit":          {data: "VALUE=long", limits: Limits{MaxLines: 10, MaxLineBytes: 4, MaxKeys: 10}},
		"export alone":             {data: "export", limits: limits},
		"export prefix":            {data: "exported=value", limits: limits},
		"missing equals":           {data: "VALUE", limits: limits},
		"invalid name":             {data: "1VALUE=bad", limits: limits},
		"key limit":                {data: "A=1\nB=2", limits: Limits{MaxLines: 10, MaxLineBytes: 100, MaxKeys: 1}},
		"single remainder":         {data: "VALUE='ok' bad", limits: limits},
		"double remainder":         {data: `VALUE="ok" bad`, limits: limits},
		"single unterminated":      {data: "VALUE='bad", limits: limits},
		"double trailing escape":   {data: `VALUE="bad\`, limits: limits},
		"unsupported escape":       {data: `VALUE="bad\q"`, limits: limits},
		"unquoted trailing escape": {data: `VALUE=bad\`, limits: limits},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := parse(context.Background(), []byte(test.data), test.limits); err == nil {
				t.Fatal("parse() error = nil")
			}
		})
	}
}

func TestParsingCoversEscapesCommentsNamesAndCancellation(t *testing.T) {
	t.Parallel()

	limits, _ := normalizeLimits(Limits{})
	records, err := parse(context.Background(), []byte(strings.Join([]string{
		"# comment",
		"export VALUE=plain # comment",
		`ESCAPES="line\nreturn\rtab\tslash\\quote\"dollar\$" # comment`,
		"_UNICODE_2=ok",
	}, "\r\n")), limits)
	if err != nil {
		t.Fatalf("parse() error = %v", err)
	}
	if len(records) != 3 || records[1].value != "line\nreturn\rtab\tslash\\quote\"dollar"+escapedDollar {
		t.Fatalf("parse() records = %#v", records)
	}
	if !validName("É_2") || validName("") || validName("A-B") || validName("2A") {
		t.Fatal("validName() boundary mismatch")
	}
	if _, err := parse(&stagedDotenvContext{}, []byte("A=1\nB=2"), limits); !errors.Is(err, context.Canceled) {
		t.Fatalf("parse() error = %v, want context.Canceled", err)
	}
}

func TestInterpolationCoversResolutionFallbackAndBounds(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		records []record
		options Interpolation
		ctx     context.Context
	}{
		"maximum depth": {
			records: []record{{name: "A", value: "${B}", interpolate: true}, {name: "B", value: "${C}", interpolate: true}, {name: "C", value: "end", interpolate: true}},
			options: Interpolation{IncludeFile: true, MaxDepth: 1, MaxExpandedBytes: 100},
		},
		"invalid expression name": {
			records: []record{{name: "A", value: "${BAD-NAME}", interpolate: true}},
			options: Interpolation{MaxDepth: 10, MaxExpandedBytes: 100},
		},
		"unterminated expression": {
			records: []record{{name: "A", value: "${B", interpolate: true}},
			options: Interpolation{MaxDepth: 10, MaxExpandedBytes: 100},
		},
		"expanded limit": {
			records: []record{{name: "A", value: "${B}", interpolate: true}},
			options: Interpolation{Variables: map[string]string{"B": "long"}, MaxDepth: 10, MaxExpandedBytes: 2},
		},
		"fallback expansion error": {
			records: []record{{name: "A", value: "${B:-${BAD-NAME}}", interpolate: true}},
			options: Interpolation{MaxDepth: 10, MaxExpandedBytes: 100},
		},
		"cancellation": {
			records: []record{{name: "A", value: "${B}", interpolate: true}},
			options: Interpolation{MaxDepth: 10, MaxExpandedBytes: 100}, ctx: canceledContext(),
		},
		"literal expanded limit": {
			records: []record{{name: "A", value: "long", interpolate: true}},
			options: Interpolation{MaxDepth: 10, MaxExpandedBytes: 2},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := test.ctx
			if ctx == nil {
				ctx = context.Background()
			}
			if err := interpolate(ctx, test.records, test.options); err == nil {
				t.Fatal("interpolate() error = nil")
			}
		})
	}

	records := []record{
		{name: "LITERAL", value: "${IGNORED}", interpolate: false},
		{name: "A", value: "${B}-${B}-${LITERAL}-${MISSING:-fallback}", interpolate: true},
	}
	options := Interpolation{
		Variables:   map[string]string{"B": "value"},
		IncludeFile: true, MaxDepth: 10, MaxExpandedBytes: 100,
	}
	if err := interpolate(context.Background(), records, options); err != nil {
		t.Fatalf("interpolate() error = %v", err)
	}
	want := []record{
		{name: "LITERAL", value: "${IGNORED}", interpolate: false},
		{name: "A", value: "value-value-${IGNORED}-fallback", interpolate: true},
	}
	if !reflect.DeepEqual(records, want) {
		t.Fatalf("interpolate() records = %#v, want %#v", records, want)
	}
}

func TestLoadPropagatesMapperAndInterpolationErrors(t *testing.T) {
	t.Parallel()

	mapperFailure := errors.New("mapper failure")
	source := &source{
		info:   config.SourceInfo{Name: "dotenv"},
		input:  sourceio.Bytes([]byte("VALUE=ok")),
		limits: Limits{MaxBytes: 100, MaxLines: 10, MaxLineBytes: 100, MaxKeys: 10},
		mapValues: func(context.Context, []string) (config.Document, error) {
			return config.Document{}, mapperFailure
		},
	}
	if _, err := source.Load(context.Background()); !errors.Is(err, mapperFailure) {
		t.Fatalf("Load() error = %v, want mapper failure", err)
	}
	source.interpolation = &Interpolation{MaxDepth: 10, MaxExpandedBytes: 100}
	source.input = sourceio.Bytes([]byte("VALUE=${MISSING}"))
	if _, err := source.Load(context.Background()); err == nil {
		t.Fatal("Load() interpolation error = nil")
	}
}

type emptyDotenvFS struct{}

func (emptyDotenvFS) Open(string) (fs.File, error) { return nil, fs.ErrNotExist }

type openDotenvFS func(string) (fs.File, error)

func (f openDotenvFS) Open(name string) (fs.File, error) { return f(name) }

type scriptedDotenvFile struct {
	reader   io.Reader
	closeErr error
}

func (f *scriptedDotenvFile) Read(buffer []byte) (int, error) { return f.reader.Read(buffer) }
func (f *scriptedDotenvFile) Close() error                    { return f.closeErr }
func (*scriptedDotenvFile) Stat() (fs.FileInfo, error)        { return dotenvFileInfo{}, nil }

type errorDotenvReader struct{ err error }

func (r errorDotenvReader) Read([]byte) (int, error) { return 0, r.err }

type dotenvFileInfo struct{}

func (dotenvFileInfo) Name() string       { return "config.env" }
func (dotenvFileInfo) Size() int64        { return 0 }
func (dotenvFileInfo) Mode() fs.FileMode  { return 0o600 }
func (dotenvFileInfo) ModTime() time.Time { return time.Time{} }
func (dotenvFileInfo) IsDir() bool        { return false }
func (dotenvFileInfo) Sys() any           { return nil }

type stagedDotenvContext struct{ calls int }

func (*stagedDotenvContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*stagedDotenvContext) Done() <-chan struct{}       { return nil }
func (*stagedDotenvContext) Value(any) any               { return nil }
func (c *stagedDotenvContext) Err() error {
	c.calls++
	if c.calls > 1 {
		return context.Canceled
	}
	return nil
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
