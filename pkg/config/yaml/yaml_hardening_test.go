package yaml

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	config "github.com/faustbrian/golib/pkg/config"
	yamlv4 "go.yaml.in/yaml/v4"
)

func TestErrorsHaveStableSecretSafeFormatting(t *testing.T) {
	t.Parallel()

	malformed := errors.New("canary-secret")
	withoutLocation := &ParseError{Cause: malformed}
	if got := withoutLocation.Error(); got != "decode YAML config: malformed document" {
		t.Fatalf("ParseError.Error() = %q", got)
	}
	if !errors.Is(withoutLocation, malformed) {
		t.Fatal("ParseError does not unwrap its cause")
	}
	if unwrapped := errors.Unwrap(withoutLocation); unwrapped == nil ||
		strings.Contains(unwrapped.Error(), "canary-secret") {
		t.Fatalf("ParseError.Unwrap() leaked cause: %v", unwrapped)
	}
	if got := fmt.Sprintf("%#v", withoutLocation); got != withoutLocation.Error() {
		t.Fatalf("formatted ParseError = %q", got)
	}
	if text, marshalErr := withoutLocation.MarshalText(); marshalErr != nil || string(text) != withoutLocation.Error() {
		t.Fatalf("ParseError.MarshalText() = %q, %v", text, marshalErr)
	}
	withLocation := (&ParseError{Line: 2, Column: 3, Cause: malformed}).Error()
	if withLocation != "decode YAML config at 2:3: malformed document" {
		t.Fatalf("located ParseError.Error() = %q", withLocation)
	}
	duplicate := (&DuplicateKeyError{Path: "server.port", Line: 3, Column: 4}).Error()
	if duplicate != `decode YAML config: duplicate key "server.port" at 3:4` {
		t.Fatalf("DuplicateKeyError.Error() = %q", duplicate)
	}
}

func TestConstructorsValidateInputsAndPreserveImmutableMetadata(t *testing.T) {
	t.Parallel()

	for name, options := range map[string]Options{
		"empty name":     {},
		"negative bytes": {Name: "yaml", Limits: Limits{MaxBytes: -1}},
		"negative depth": {Name: "yaml", Limits: Limits{MaxDepth: -1}},
		"negative keys":  {Name: "yaml", Limits: Limits{MaxKeys: -1}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := Bytes(nil, options); err == nil {
				t.Fatal("Bytes() error = nil")
			}
		})
	}
	if _, err := FromFS(nil, "config.yaml", Options{Name: "yaml"}); err == nil {
		t.Fatal("FromFS(nil) error = nil")
	}
	if _, err := FromFS(emptyYAMLFS{}, "../config.yaml", Options{Name: "yaml"}); err == nil {
		t.Fatal("FromFS(invalid path) error = nil")
	}
	if _, err := FromFS(emptyYAMLFS{}, "config.yaml", Options{}); err == nil {
		t.Fatal("FromFS(invalid options) error = nil")
	}

	data := []byte("value: original\n")
	source, err := Bytes(data, Options{
		Name: "yaml", Priority: 42, Sensitive: true, Optional: true,
	})
	if err != nil {
		t.Fatalf("Bytes() error = %v", err)
	}
	data[7] = 'X'
	if got := source.Info(); got != (config.SourceInfo{
		Name: "yaml", Priority: 42, Sensitive: true, Optional: true,
	}) {
		t.Fatalf("Info() = %#v", got)
	}
	document, err := source.Load(context.Background())
	if err != nil || document.Tree["value"] != "original" {
		t.Fatalf("Load() = %#v, %v", document, err)
	}
}

func TestSourceHandlesEmptyInputAndPropagatesFilesystemFailures(t *testing.T) {
	t.Parallel()

	for _, data := range [][]byte{nil, []byte(" \n\t")} {
		source, err := Bytes(data, Options{Name: "yaml"})
		if err != nil {
			t.Fatalf("Bytes() error = %v", err)
		}
		document, err := source.Load(context.Background())
		if err != nil || len(document.Tree) != 0 {
			t.Fatalf("Load() = %#v, %v", document, err)
		}
	}

	readFailure := errors.New("read failure")
	closeFailure := errors.New("close failure")
	tests := map[string]struct {
		filesystem fs.FS
		limits     Limits
		want       error
	}{
		"read": {
			filesystem: openYAMLFS(func(string) (fs.File, error) {
				return &scriptedYAMLFile{reader: errorYAMLReader{err: readFailure}}, nil
			}),
			want: readFailure,
		},
		"close": {
			filesystem: openYAMLFS(func(string) (fs.File, error) {
				return &scriptedYAMLFile{reader: strings.NewReader("{}"), closeErr: closeFailure}, nil
			}),
			want: closeFailure,
		},
		"limit": {
			filesystem: openYAMLFS(func(string) (fs.File, error) {
				return &scriptedYAMLFile{reader: strings.NewReader("long: value\n")}, nil
			}),
			limits: Limits{MaxBytes: 4},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			source, err := FromFS(test.filesystem, "config.yaml", Options{
				Name: "yaml", Limits: test.limits,
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

func TestSourceRejectsNonMappingRoot(t *testing.T) {
	t.Parallel()

	source, err := Bytes([]byte("- one\n- two\n"), Options{Name: "yaml"})
	if err != nil {
		t.Fatalf("Bytes() error = %v", err)
	}
	if _, err := source.Load(context.Background()); err == nil {
		t.Fatal("Load() error = nil")
	}
}

func TestConvertRejectsMalformedAndUnsupportedNodes(t *testing.T) {
	t.Parallel()

	scalarNode := func(value string) *yamlv4.Node {
		return &yamlv4.Node{Kind: yamlv4.ScalarNode, Tag: "!!str", Value: value}
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	keys := 0
	if _, err := convert(
		canceled,
		scalarNode("value"),
		1,
		"",
		Limits{MaxDepth: 10, MaxKeys: 10},
		&keys,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("convert(canceled) error = %v", err)
	}
	tests := map[string]struct {
		node   *yamlv4.Node
		depth  int
		limits Limits
	}{
		"depth": {
			node: scalarNode("value"), depth: 2, limits: Limits{MaxDepth: 1},
		},
		"alias": {node: &yamlv4.Node{Kind: yamlv4.AliasNode}, depth: 1},
		"tagged map": {
			node: &yamlv4.Node{Kind: yamlv4.MappingNode, Tag: "!map"}, depth: 1,
		},
		"odd map": {
			node:  &yamlv4.Node{Kind: yamlv4.MappingNode, Tag: "!!map", Content: []*yamlv4.Node{scalarNode("key")}},
			depth: 1,
		},
		"merge key": {
			node: &yamlv4.Node{
				Kind: yamlv4.MappingNode,
				Tag:  "!!map",
				Content: []*yamlv4.Node{
					{Kind: yamlv4.ScalarNode, Tag: "!!str", Value: "<<"},
					scalarNode("value"),
				},
			},
			depth: 1,
		},
		"tagged sequence": {
			node: &yamlv4.Node{Kind: yamlv4.SequenceNode, Tag: "!sequence"}, depth: 1,
		},
		"invalid sequence child": {
			node:  &yamlv4.Node{Kind: yamlv4.SequenceNode, Tag: "!!seq", Content: []*yamlv4.Node{{Kind: yamlv4.AliasNode}}},
			depth: 1,
		},
		"unsupported node": {node: &yamlv4.Node{Kind: yamlv4.DocumentNode}, depth: 1},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			limits := test.limits
			if limits.MaxDepth == 0 {
				limits.MaxDepth = 10
			}
			keys := 0
			if _, err := convert(context.Background(), test.node, test.depth, "root", limits, &keys); err == nil {
				t.Fatal("convert() error = nil")
			}
		})
	}
}

func TestScalarAndIntegerBoundaries(t *testing.T) {
	t.Parallel()

	for name, node := range map[string]*yamlv4.Node{
		"invalid bool":   {Kind: yamlv4.ScalarNode, Tag: "!!bool", Value: "maybe"},
		"infinite float": {Kind: yamlv4.ScalarNode, Tag: "!!float", Value: ".inf"},
		"nan float":      {Kind: yamlv4.ScalarNode, Tag: "!!float", Value: "NaN"},
		"custom tag":     {Kind: yamlv4.ScalarNode, Tag: "!secret", Value: "canary"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := scalar(node); err == nil {
				t.Fatal("scalar() error = nil")
			}
		})
	}
	stamp := &yamlv4.Node{
		Kind: yamlv4.ScalarNode, Tag: "!!timestamp", Value: "2026-07-15",
	}
	if got, err := scalar(stamp); err != nil || got != stamp.Value {
		t.Fatalf("scalar(timestamp) = %#v, %v", got, err)
	}

	want := map[string]any{
		"decimal":  int64(-42),
		"hex":      int64(255),
		"octal":    int64(8),
		"binary":   int64(3),
		"unsigned": uint64(math.MaxUint64),
	}
	inputs := map[string]string{
		"decimal": "-4_2", "hex": "0xFF", "octal": "0o10",
		"binary": "0b11", "unsigned": "+18446744073709551615",
	}
	got := make(map[string]any, len(inputs))
	for name, input := range inputs {
		value, err := parseInteger(input)
		if err != nil {
			t.Fatalf("parseInteger(%q) error = %v", input, err)
		}
		got[name] = value
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("integer values = %#v, want %#v", got, want)
	}
	if _, err := parseInteger("18446744073709551616"); err == nil {
		t.Fatal("parseInteger(overflow) error = nil")
	}
}

func TestSourceHonorsCancellationDuringConversion(t *testing.T) {
	t.Parallel()

	source, err := Bytes([]byte("outer:\n  inner: true\n"), Options{Name: "yaml"})
	if err != nil {
		t.Fatalf("Bytes() error = %v", err)
	}
	_, err = source.Load(&stagedYAMLContext{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Load() error = %v, want context.Canceled", err)
	}
}

type stagedYAMLContext struct{ calls int }

func (*stagedYAMLContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*stagedYAMLContext) Done() <-chan struct{}       { return nil }
func (*stagedYAMLContext) Value(any) any               { return nil }
func (c *stagedYAMLContext) Err() error {
	c.calls++
	if c.calls > 1 {
		return context.Canceled
	}
	return nil
}

type emptyYAMLFS struct{}

func (emptyYAMLFS) Open(string) (fs.File, error) { return nil, fs.ErrNotExist }

type openYAMLFS func(string) (fs.File, error)

func (f openYAMLFS) Open(name string) (fs.File, error) { return f(name) }

type scriptedYAMLFile struct {
	reader   io.Reader
	closeErr error
}

func (f *scriptedYAMLFile) Read(buffer []byte) (int, error) { return f.reader.Read(buffer) }
func (f *scriptedYAMLFile) Close() error                    { return f.closeErr }
func (*scriptedYAMLFile) Stat() (fs.FileInfo, error)        { return yamlFileInfo{}, nil }

type errorYAMLReader struct{ err error }

func (r errorYAMLReader) Read([]byte) (int, error) { return 0, r.err }

type yamlFileInfo struct{}

func (yamlFileInfo) Name() string       { return "config.yaml" }
func (yamlFileInfo) Size() int64        { return 0 }
func (yamlFileInfo) Mode() fs.FileMode  { return 0o600 }
func (yamlFileInfo) ModTime() time.Time { return time.Time{} }
func (yamlFileInfo) IsDir() bool        { return false }
func (yamlFileInfo) Sys() any           { return nil }
