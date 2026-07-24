package resolve_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/xsd/resolve"
)

func TestFileResolverReadsOnlyWithinItsConfiguredRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "schemas", "value.xsd")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`<schema/>`), 0o600); err != nil {
		t.Fatal(err)
	}
	resolver, err := resolve.NewFile(resolve.FileOptions{Root: root, MaxBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resolver.Close() })
	identity := (&url.URL{Scheme: "file", Path: path}).String()
	resource, err := resolver.Resolve(context.Background(), resolve.Request{URI: identity})
	if err != nil {
		t.Fatal(err)
	}
	if resource.URI != identity || string(resource.Content) != `<schema/>` {
		t.Fatalf("Resource = %#v", resource)
	}
	resource.Content[0] = '!'
	again, err := resolver.Resolve(context.Background(), resolve.Request{URI: identity})
	if err != nil || string(again.Content) != `<schema/>` {
		t.Fatalf("second Resolve() = %#v, %v", again, err)
	}
}

func TestFileResolverSupportsEscapedFileNames(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "percent%.xsd")
	if err := os.WriteFile(path, []byte("schema"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolver, err := resolve.NewFile(resolve.FileOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resolver.Close() })
	identity := (&url.URL{Scheme: "file", Path: path}).String()
	resource, err := resolver.Resolve(context.Background(), resolve.Request{URI: identity})
	if err != nil || string(resource.Content) != "schema" {
		t.Fatalf("Resolve() = %#v, %v", resource, err)
	}
}

func TestFileResolverRejectsUnsafeAndOversizedResources(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.xsd")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(root, "large.xsd")
	if err := os.WriteFile(inside, []byte("too large"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolver, err := resolve.NewFile(resolve.FileOptions{Root: root, MaxBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resolver.Close() })
	for _, identity := range []string{
		"https://example.test/schema.xsd",
		"file://host/schema.xsd",
		"file:relative.xsd",
		"file://",
		"file:///C:/outside.xsd",
		"file:///schema.xsd?query",
		"file:///schema.xsd#fragment",
		"file:///invalid%zz",
		(&url.URL{Scheme: "file", Path: outside}).String(),
	} {
		if _, err := resolver.Resolve(context.Background(), resolve.Request{URI: identity}); !errors.Is(err, resolve.ErrAccessDenied) {
			t.Fatalf("Resolve(%q) error = %v", identity, err)
		}
	}
	identity := (&url.URL{Scheme: "file", Path: inside}).String()
	if _, err := resolver.Resolve(context.Background(), resolve.Request{URI: identity}); !errors.Is(err, resolve.ErrLimitExceeded) {
		t.Fatalf("Resolve(oversized) error = %v", err)
	}
	missing := (&url.URL{Scheme: "file", Path: filepath.Join(root, "missing.xsd")}).String()
	if _, err := resolver.Resolve(context.Background(), resolve.Request{URI: missing}); !errors.Is(err, resolve.ErrNotFound) {
		t.Fatalf("Resolve(missing) error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := resolver.Resolve(canceled, resolve.Request{URI: identity}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve(canceled) error = %v", err)
	}
}

func TestFileResolverRejectsSymlinkEscape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.xsd")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.xsd")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	resolver, err := resolve.NewFile(resolve.FileOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resolver.Close() })
	identity := (&url.URL{Scheme: "file", Path: link}).String()
	if _, err := resolver.Resolve(context.Background(), resolve.Request{URI: identity}); !errors.Is(err, resolve.ErrAccessDenied) {
		t.Fatalf("Resolve(symlink escape) error = %v", err)
	}
}

func TestFileResolverReportsUnreadableResources(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	resolver, err := resolve.NewFile(resolve.FileOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	identity := (&url.URL{Scheme: "file", Path: root}).String()
	if _, err := resolver.Resolve(context.Background(), resolve.Request{URI: identity}); err == nil {
		t.Fatal("Resolve(directory) succeeded")
	}
	if err := resolver.Close(); err != nil {
		t.Fatal(err)
	}
	missing := (&url.URL{Scheme: "file", Path: filepath.Join(root, "schema.xsd")}).String()
	if _, err := resolver.Resolve(context.Background(), resolve.Request{URI: missing}); !errors.Is(err, resolve.ErrAccessDenied) {
		t.Fatalf("Resolve(closed) error = %v", err)
	}
	var nilResolver *resolve.File
	if err := nilResolver.Close(); err != nil {
		t.Fatalf("nil Close() error = %v", err)
	}
	if _, err := nilResolver.Resolve(context.Background(), resolve.Request{}); !errors.Is(err, resolve.ErrAccessDenied) {
		t.Fatalf("nil Resolve() error = %v", err)
	}
}

func TestFileResolverObservesCancellationAfterReading(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "schema.xsd")
	if err := os.WriteFile(path, []byte("schema"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolver, err := resolve.NewFile(resolve.FileOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resolver.Close() })
	identity := (&url.URL{Scheme: "file", Path: path}).String()
	ctx := &cancelAfterFirstCheck{}
	if _, err := resolver.Resolve(ctx, resolve.Request{URI: identity}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve() error = %v", err)
	}
}

type cancelAfterFirstCheck struct {
	checks int
}

func (*cancelAfterFirstCheck) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*cancelAfterFirstCheck) Done() <-chan struct{}       { return nil }
func (*cancelAfterFirstCheck) Value(any) any               { return nil }

func (c *cancelAfterFirstCheck) Err() error {
	c.checks++
	if c.checks > 1 {
		return context.Canceled
	}
	return nil
}

func TestNewFileRejectsInvalidOptions(t *testing.T) {
	t.Parallel()

	for _, options := range []resolve.FileOptions{
		{},
		{Root: "relative"},
		{Root: t.TempDir(), MaxBytes: -1},
		{Root: t.TempDir(), MaxBytes: int64(^uint64(0) >> 1)},
	} {
		if _, err := resolve.NewFile(options); err == nil {
			t.Fatalf("NewFile(%+v) accepted invalid options", options)
		}
	}
	if _, err := resolve.NewFile(resolve.FileOptions{Root: filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Fatal("NewFile() accepted a missing root")
	}
}

func TestCatalogResolvesLocationlessImportsByNamespace(t *testing.T) {
	t.Parallel()

	resources, err := resolve.NewMemory(map[string][]byte{
		"urn:schema:dependency": []byte(`<schema/>`),
		"urn:schema:direct":     []byte(`<direct/>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	namespaces := map[string]string{"urn:dependency": "urn:schema:dependency"}
	catalog, err := resolve.NewCatalog(namespaces, resources)
	if err != nil {
		t.Fatal(err)
	}
	namespaces["urn:dependency"] = "urn:mutated"
	resource, err := catalog.Resolve(context.Background(), resolve.Request{
		Namespace: "urn:dependency",
		Kind:      resolve.KindImport,
	})
	if err != nil || resource.URI != "urn:schema:dependency" || string(resource.Content) != `<schema/>` {
		t.Fatalf("Resolve(import) = %#v, %v", resource, err)
	}
	direct, err := catalog.Resolve(context.Background(), resolve.Request{
		URI:  "urn:schema:direct",
		Kind: resolve.KindInclude,
	})
	if err != nil || string(direct.Content) != `<direct/>` {
		t.Fatalf("Resolve(direct) = %#v, %v", direct, err)
	}
}

func TestCatalogRejectsInvalidAndUnknownMappings(t *testing.T) {
	t.Parallel()

	resources, err := resolve.NewMemory(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, namespaces := range []map[string]string{
		{"urn:test": "relative.xsd"},
		{"urn:test": "urn:schema#fragment"},
	} {
		if _, err := resolve.NewCatalog(namespaces, resources); err == nil {
			t.Fatalf("NewCatalog(%v) succeeded", namespaces)
		}
	}
	if _, err := resolve.NewCatalog(nil, nil); err == nil {
		t.Fatal("NewCatalog(nil resolver) succeeded")
	}
	catalog, err := resolve.NewCatalog(nil, resources)
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []resolve.Request{
		{Namespace: "urn:missing", Kind: resolve.KindImport},
		{Namespace: "urn:missing", Kind: resolve.KindInclude},
	} {
		if _, err := catalog.Resolve(context.Background(), request); !errors.Is(err, resolve.ErrNotFound) {
			t.Fatalf("Resolve(%+v) error = %v", request, err)
		}
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.Resolve(canceled, resolve.Request{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve(canceled) error = %v", err)
	}
	mapped, err := resolve.NewCatalog(
		map[string]string{"urn:test": "urn:missing"},
		resources,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mapped.Resolve(context.Background(), resolve.Request{
		Namespace: "urn:test",
		Kind:      resolve.KindImport,
	}); !errors.Is(err, resolve.ErrNotFound) {
		t.Fatalf("Resolve(missing target) error = %v", err)
	}
	mismatched, err := resolve.NewCatalog(
		map[string]string{"urn:test": "urn:expected"},
		resolverFunc(func(context.Context, resolve.Request) (resolve.Resource, error) {
			return resolve.Resource{URI: "urn:other"}, nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mismatched.Resolve(context.Background(), resolve.Request{
		Namespace: "urn:test",
		Kind:      resolve.KindImport,
	}); err == nil {
		t.Fatal("Resolve(mismatched identity) succeeded")
	}
}

type resolverFunc func(context.Context, resolve.Request) (resolve.Resource, error)

func (f resolverFunc) Resolve(ctx context.Context, request resolve.Request) (resolve.Resource, error) {
	return f(ctx, request)
}

func TestDenyResolverRejectsEveryResource(t *testing.T) {
	t.Parallel()

	_, err := resolve.Deny().Resolve(context.Background(), resolve.Request{
		URI:  "file:///etc/passwd",
		Kind: resolve.KindInclude,
	})
	if !errors.Is(err, resolve.ErrAccessDenied) {
		t.Fatalf("Resolve() error = %v, want ErrAccessDenied", err)
	}
}

func TestMemoryResolverReturnsOwnedResourceCopies(t *testing.T) {
	t.Parallel()

	source := []byte(`<schema/>`)
	resolver, err := resolve.NewMemory(map[string][]byte{
		"https://example.test/common.xsd": source,
	})
	if err != nil {
		t.Fatalf("NewMemory() error = %v", err)
	}
	source[0] = '!'

	request := resolve.Request{
		URI:       "https://example.test/common.xsd",
		Namespace: "urn:example",
		Kind:      resolve.KindInclude,
	}
	resource, err := resolver.Resolve(context.Background(), request)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if string(resource.Content) != `<schema/>` {
		t.Fatalf("Content = %q", resource.Content)
	}
	resource.Content[0] = '!'

	again, err := resolver.Resolve(context.Background(), request)
	if err != nil {
		t.Fatalf("second Resolve() error = %v", err)
	}
	if string(again.Content) != `<schema/>` {
		t.Fatalf("second Content = %q", again.Content)
	}
	if again.URI != request.URI {
		t.Fatalf("URI = %q", again.URI)
	}
}

func TestMemoryResolverHonorsCancellationAndMissingResources(t *testing.T) {
	t.Parallel()

	resolver, err := resolve.NewMemory(nil)
	if err != nil {
		t.Fatalf("NewMemory() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = resolver.Resolve(ctx, resolve.Request{URI: "urn:missing"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Resolve() error = %v", err)
	}

	_, err = resolver.Resolve(context.Background(), resolve.Request{URI: "urn:missing"})
	if !errors.Is(err, resolve.ErrNotFound) {
		t.Fatalf("missing Resolve() error = %v", err)
	}
}

func TestResolversRejectInvalidConfigurationAndHonorCancellation(t *testing.T) {
	t.Parallel()

	for _, identity := range []string{"relative.xsd", "https://example.test/schema.xsd#part", "://bad"} {
		if _, err := resolve.NewMemory(map[string][]byte{identity: nil}); err == nil {
			t.Fatalf("NewMemory(%q) succeeded", identity)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := resolve.Deny().Resolve(ctx, resolve.Request{URI: "urn:test"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Deny.Resolve() error = %v", err)
	}
}

func TestChainTriesOnlyMissingResolvers(t *testing.T) {
	t.Parallel()

	memory, err := resolve.NewMemory(map[string][]byte{"urn:found": []byte("schema")})
	if err != nil {
		t.Fatal(err)
	}
	resource, err := resolve.Chain(nil, memory).Resolve(
		context.Background(),
		resolve.Request{URI: "urn:found"},
	)
	if err != nil || string(resource.Content) != "schema" {
		t.Fatalf("Resolve(found) = %#v, %v", resource, err)
	}
	if _, err := resolve.Chain(memory).Resolve(
		context.Background(),
		resolve.Request{URI: "urn:missing"},
	); !errors.Is(err, resolve.ErrNotFound) {
		t.Fatalf("Resolve(missing) error = %v", err)
	}
	if _, err := resolve.Chain(memory, resolve.Deny()).Resolve(
		context.Background(),
		resolve.Request{URI: "urn:missing"},
	); !errors.Is(err, resolve.ErrAccessDenied) {
		t.Fatalf("Resolve(denied) error = %v", err)
	}
}
