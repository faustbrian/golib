package reference

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

func TestPointerCoversExactLimitsAndEveryTargetFailure(t *testing.T) {
	t.Parallel()

	for _, policy := range []PointerPolicy{
		{MaxLength: 0, MaxTokens: 1, MaxIndexDigits: 1},
		{MaxLength: 1, MaxTokens: 0, MaxIndexDigits: 1},
		{MaxLength: 1, MaxTokens: 1, MaxIndexDigits: 0},
	} {
		if _, err := ParsePointer("", policy); !errors.Is(err, ErrPointerPolicy) {
			t.Errorf("invalid policy error = %v", err)
		}
	}
	policy := PointerPolicy{MaxLength: 2, MaxTokens: 1, MaxIndexDigits: 1}
	pointer, err := ParsePointer("/0", policy)
	if err != nil {
		t.Fatal(err)
	}
	array := referenceValue(t, `[1]`)
	if _, err := pointer.Evaluate(array, jsonvalue.DefaultPolicy()); err != nil {
		t.Fatalf("exact pointer limits error = %v", err)
	}
	for _, fragment := range []string{"", "plain", "#%zz"} {
		if _, err := ParseFragment(fragment, DefaultPointerPolicy()); !errors.Is(err, ErrInvalidPointer) {
			t.Errorf("ParseFragment(%q) error = %v", fragment, err)
		}
	}
	nonempty, err := ParsePointer("/value", DefaultPointerPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nonempty.Evaluate(jsonvalue.Value{}, jsonvalue.DefaultPolicy()); !errors.Is(err, ErrPointerTarget) {
		t.Fatalf("zero target error = %v", err)
	}
	invalidJSONPolicy := jsonvalue.DefaultPolicy()
	invalidJSONPolicy.MaxBytes = 0
	if _, err := pointer.Evaluate(array, invalidJSONPolicy); !errors.Is(err, jsonvalue.ErrInvalidPolicy) {
		t.Fatalf("target policy error = %v", err)
	}
	for _, token := range []string{"", "-", "01", "x", "999999999999999999999999"} {
		pointer := Pointer{maxIndexDigits: 30}
		if _, err := pointer.arrayIndex(token); err == nil {
			t.Errorf("arrayIndex(%q) succeeded", token)
		}
	}
	if _, err := (Pointer{maxIndexDigits: 1}).arrayIndex("10"); !errors.Is(err, ErrPointerLimit) {
		t.Fatalf("index digit limit error = %v", err)
	}
	if index, err := (Pointer{maxIndexDigits: 1}).arrayIndex("9"); err != nil || index != 9 {
		t.Fatalf("maximum digit index = %d, %v", index, err)
	}
}

func TestReferenceCoversZeroValuesControlsAndResolvedLimits(t *testing.T) {
	t.Parallel()

	if _, err := (Reference{}).ResolveAgainst("https://example.com/root"); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("zero resolve error = %v", err)
	}
	if _, err := (Reference{}).TargetPointer(DefaultPointerPolicy()); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("zero target pointer error = %v", err)
	}
	reference, err := Parse("child", Policy{MaxLength: 5})
	if err != nil {
		t.Fatal(err)
	}
	for _, base := range []string{"", "relative", "https://example.com/\n"} {
		if _, err := reference.ResolveAgainst(base); !errors.Is(err, ErrInvalidBase) {
			t.Errorf("ResolveAgainst(%q) error = %v", base, err)
		}
	}
	if _, err := reference.ResolveAgainst("https://example.com/root"); !errors.Is(err, ErrReferenceLimit) {
		t.Fatalf("resolved length error = %v", err)
	}
	badFragment, err := Parse("#plain", DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := badFragment.TargetPointer(DefaultPointerPolicy()); !errors.Is(err, ErrInvalidReference) {
		t.Fatalf("fragment pointer error = %v", err)
	}
	for _, value := range []string{"\x00", " ", "\x7f"} {
		if !containsURIControl(value) {
			t.Errorf("containsURIControl(%q) = false", value)
		}
	}
}

func TestMemoryAndFilesystemStoresCoverEveryFailure(t *testing.T) {
	t.Parallel()

	if _, err := NewMemoryStore(map[string][]byte{"https://example.com/a": {}, "https://example.com/a#": {}}); !errors.Is(err, ErrStoreURI) {
		t.Fatalf("duplicate canonical URI error = %v", err)
	}
	store, err := NewMemoryStore(map[string][]byte{"https://example.com/a": []byte("a")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (*MemoryStore)(nil).Load(context.Background(), "https://example.com/a", 1); !errors.Is(err, ErrStorePolicy) {
		t.Fatalf("nil memory store error = %v", err)
	}
	for _, uri := range []string{"relative", "https://example.com/missing"} {
		if _, err := store.Load(context.Background(), uri, 1); !errors.Is(err, ErrStoreURI) {
			t.Errorf("memory Load(%q) error = %v", uri, err)
		}
	}
	if data, err := store.Load(context.Background(), "https://example.com/a", 1); err != nil || string(data) != "a" {
		t.Fatalf("exact memory load = %q, %v", data, err)
	}

	invalidBases := []string{"relative/", "https://example.com/base", "https://example.com/base/?q=1", "https://user@example.com/base/", "https://example.com/base/#fragment"}
	for _, base := range invalidBases {
		if _, err := NewFSStore(fstest.MapFS{}, base); !errors.Is(err, ErrStorePolicy) {
			t.Errorf("NewFSStore(%q) error = %v", base, err)
		}
	}
	fsStore, err := NewFSStore(fstest.MapFS{"a.json": {Data: []byte("a")}}, "https://example.com/base/")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (*FSStore)(nil).Load(context.Background(), "https://example.com/base/a.json", 1); !errors.Is(err, ErrStorePolicy) {
		t.Fatalf("nil filesystem store error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fsStore.Load(canceled, "https://example.com/base/a.json", 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("filesystem cancellation error = %v", err)
	}
	for _, uri := range []string{
		"https://example.com/base/", "https://example.com/base/../a.json",
		"https://example.com/other/a.json", "https://example.com/base/missing.json",
	} {
		if _, err := fsStore.Load(context.Background(), uri, 1); err == nil {
			t.Errorf("filesystem Load(%q) succeeded", uri)
		}
	}
	if data, err := fsStore.Load(context.Background(), "https://example.com/base/a.json", 1); err != nil || string(data) != "a" {
		t.Fatalf("exact filesystem load = %q, %v", data, err)
	}
	broken, err := NewFSStore(errorFS{}, "https://example.com/base/")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := broken.Load(context.Background(), "https://example.com/base/a.json", 1); !errors.Is(err, ErrStoreRead) {
		t.Fatalf("read/close error = %v", err)
	}
	ctx := &referenceCountingContext{Context: context.Background(), remaining: 1}
	if _, err := fsStore.Load(ctx, "https://example.com/base/a.json", 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("post-read cancellation error = %v", err)
	}
}

func TestBundleRejectsInvalidSetupAndRoot(t *testing.T) {
	t.Parallel()

	resolver, err := NewResolver(nil, DefaultResolvePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Bundle(context.Background(), nil, jsonvalue.Value{}, "https://example.com/root"); !errors.Is(err, ErrResolvePolicy) {
		t.Fatalf("nil resolver error = %v", err)
	}
	if _, err := Bundle(context.Background(), resolver, jsonvalue.Value{}, "relative"); err == nil {
		t.Fatal("relative bundle base succeeded")
	}
	if _, err := Bundle(context.Background(), resolver, jsonvalue.Value{}, "https://example.com/root"); !errors.Is(err, ErrInvalidDocument) {
		t.Fatalf("zero bundle root error = %v", err)
	}
	root := referenceValue(t, `{"$ref":"child.json"}`)
	if _, err := Bundle(context.Background(), resolver, root, "https://example.com/root"); !errors.Is(err, ErrExternalDisabled) {
		t.Fatalf("bundle resource error = %v", err)
	}
}

type errorFS struct{}

func (errorFS) Open(string) (fs.File, error) { return &errorFile{}, nil }

type errorFile struct{ strings.Reader }

func (errorFile) Stat() (fs.FileInfo, error) { return nil, errors.New("stat") }
func (errorFile) Close() error               { return errors.New("close") }

type referenceCountingContext struct {
	context.Context
	remaining int
}

func (ctx *referenceCountingContext) Err() error {
	ctx.remaining--
	if ctx.remaining < 0 {
		return context.Canceled
	}
	return nil
}

func (ctx *referenceCountingContext) Deadline() (time.Time, bool) { return time.Time{}, false }

func referenceValue(t *testing.T, source string) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.Parse([]byte(source), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return value
}

var _ io.Reader = &errorFile{}
