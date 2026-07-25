package reference_test

import (
	"context"
	"errors"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/parse"
	"github.com/faustbrian/golib/pkg/openapi/reference"
)

func TestFileResolverReadsAllowedJSONAndYAML(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	jsonPath := writeResolverFixture(t, root, "schema.json", `{"type":"string"}`)
	yamlPath := writeResolverFixture(t, root, "schema.yaml", "type: integer\n")
	resolver, err := reference.NewFileResolver(reference.FileResolverOptions{
		AllowedRoots: []string{root},
		MaxBytes:     1024,
		MaxDocuments: 2,
		ParseLimits:  parse.DefaultLimits(),
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{jsonPath, yamlPath} {
		identifier := (&url.URL{Scheme: "file", Path: path}).String()
		resource, resolveErr := resolver.Resolve(context.Background(), identifier)
		if resolveErr != nil {
			t.Fatal(resolveErr)
		}
		canonicalPath, canonicalErr := filepath.EvalSymlinks(path)
		if canonicalErr != nil {
			t.Fatal(canonicalErr)
		}
		if resource.RetrievalURI != identifier ||
			resource.CanonicalURI != fileIdentifier(canonicalPath) {
			t.Fatalf("resource identity = %#v", resource)
		}
		if value, exists := resource.Root.Lookup("type"); !exists {
			t.Fatal("resolved resource has no type")
		} else if _, valid := value.Text(); !valid {
			t.Fatalf("resolved type = %#v", value)
		}
	}
}

func TestFileResolverRejectsUnauthorizedIdentifiers(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	inside := writeResolverFixture(t, root, "inside.json", `{}`)
	outsideRoot := t.TempDir()
	outside := writeResolverFixture(t, outsideRoot, "outside.json", `{}`)
	resolver := mustFileResolver(t, root, 8)

	identifiers := []string{
		"https://example.test/schema.json",
		"schema.json",
		"file://server.example.test/schema.json",
		"file://user@/schema.json",
		(&url.URL{Scheme: "file", Path: inside, RawQuery: "secret=1"}).String(),
		(&url.URL{Scheme: "file", Path: outside}).String(),
	}
	for _, identifier := range identifiers {
		if _, err := resolver.Resolve(
			context.Background(), identifier,
		); !errors.Is(err, reference.ErrResourceDenied) {
			t.Fatalf("identifier %q error = %v", identifier, err)
		}
	}
}

func TestFileResolverRejectsSymlinkEscapes(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges differ on Windows")
	}

	root := t.TempDir()
	outside := writeResolverFixture(t, t.TempDir(), "outside.json", `{}`)
	link := filepath.Join(root, "escape.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	resolver := mustFileResolver(t, root, 1)
	identifier := (&url.URL{Scheme: "file", Path: link}).String()
	if _, err := resolver.Resolve(
		context.Background(), identifier,
	); !errors.Is(err, reference.ErrResourceDenied) {
		t.Fatalf("symlink escape error = %v", err)
	}
}

func TestFileResolverAllowsSymlinksContainedWithinRoot(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges differ on Windows")
	}

	root := t.TempDir()
	target := writeResolverFixture(t, root, "target.json", `{}`)
	link := filepath.Join(root, "alias.json")
	if err := os.Symlink(filepath.Base(target), link); err != nil {
		t.Fatal(err)
	}
	resolver := mustFileResolver(t, root, 1)
	resource, err := resolver.Resolve(context.Background(), fileIdentifier(link))
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if resource.CanonicalURI != fileIdentifier(canonical) {
		t.Fatalf("canonical URI = %q", resource.CanonicalURI)
	}
}

func TestFileResolverEnforcesResourceLimits(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	first := writeResolverFixture(t, root, "first.json", `{}`)
	second := writeResolverFixture(t, root, "second.json", `{}`)
	resolver := mustFileResolver(t, root, 1)
	if _, err := resolver.Resolve(
		context.Background(), fileIdentifier(first),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(
		context.Background(), fileIdentifier(second),
	); !errors.Is(err, reference.ErrResourceLimitExceeded) {
		t.Fatalf("document limit error = %v", err)
	}

	large := writeResolverFixture(t, root, "large.json", `{"value":"large"}`)
	limits := parse.DefaultLimits()
	byteResolver, err := reference.NewFileResolver(reference.FileResolverOptions{
		AllowedRoots: []string{root}, MaxBytes: 4, MaxDocuments: 1,
		ParseLimits: limits,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := byteResolver.Resolve(
		context.Background(), fileIdentifier(large),
	); !errors.Is(err, parse.ErrLimitExceeded) {
		t.Fatalf("byte limit error = %v", err)
	}
}

func TestFileResolverAcceptsExactByteAndConfigurationLimits(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := writeResolverFixture(t, root, "exact.json", `{}`)
	limits := parse.DefaultLimits()
	resolver, err := reference.NewFileResolver(reference.FileResolverOptions{
		AllowedRoots: []string{root}, MaxBytes: 2, MaxDocuments: 1,
		ParseLimits: limits,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := resolver.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	if _, err = resolver.Resolve(
		context.Background(), fileIdentifier(path),
	); err != nil {
		t.Fatalf("exact byte limit error = %v", err)
	}

	minimum, err := reference.NewFileResolver(reference.FileResolverOptions{
		AllowedRoots: []string{root}, MaxBytes: 1, MaxDocuments: 1,
		ParseLimits: limits,
	})
	if err != nil {
		t.Fatalf("minimum configuration error = %v", err)
	}
	if err := minimum.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFileResolverRejectsInvalidConfigurationAndInput(t *testing.T) {
	t.Parallel()

	validRoot := t.TempDir()
	fileRoot := writeResolverFixture(t, validRoot, "not-directory", `{}`)
	limits := parse.DefaultLimits()
	for _, options := range []reference.FileResolverOptions{
		{MaxBytes: 1, MaxDocuments: 1, ParseLimits: limits},
		{AllowedRoots: []string{validRoot}, MaxDocuments: 1, ParseLimits: limits},
		{AllowedRoots: []string{validRoot}, MaxBytes: math.MaxInt64, MaxDocuments: 1, ParseLimits: limits},
		{AllowedRoots: []string{validRoot}, MaxBytes: 1, ParseLimits: limits},
		{AllowedRoots: []string{""}, MaxBytes: 1, MaxDocuments: 1, ParseLimits: limits},
		{AllowedRoots: []string{filepath.Join(validRoot, "missing")}, MaxBytes: 1, MaxDocuments: 1, ParseLimits: limits},
		{AllowedRoots: []string{fileRoot}, MaxBytes: 1, MaxDocuments: 1, ParseLimits: limits},
		{AllowedRoots: []string{validRoot}, MaxBytes: 1, MaxDocuments: 1},
	} {
		if _, err := reference.NewFileResolver(options); err == nil {
			t.Fatalf("invalid options accepted: %#v", options)
		}
	}

	resolver := mustFileResolver(t, validRoot, 3)
	missing := filepath.Join(validRoot, "missing.json")
	if _, err := resolver.Resolve(
		context.Background(), fileIdentifier(missing),
	); err == nil {
		t.Fatal("missing resource was accepted")
	}
	if _, err := resolver.Resolve(
		context.Background(), fileIdentifier(validRoot),
	); !errors.Is(err, reference.ErrUnsupportedResourceFormat) {
		t.Fatalf("directory error = %v", err)
	}
	unsupported := writeResolverFixture(t, validRoot, "schema.txt", `{}`)
	if _, err := resolver.Resolve(
		context.Background(), fileIdentifier(unsupported),
	); !errors.Is(err, reference.ErrUnsupportedResourceFormat) {
		t.Fatalf("format error = %v", err)
	}
	directory := filepath.Join(validRoot, "directory.json")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(
		context.Background(), fileIdentifier(directory),
	); !errors.Is(err, reference.ErrResourceDenied) {
		t.Fatalf("non-regular resource error = %v", err)
	}
	//lint:ignore SA1012 This assertion verifies the nil-context contract.
	//nolint:staticcheck // This assertion verifies the nil-context contract.
	if _, err := resolver.Resolve(nil, fileIdentifier(unsupported)); err == nil {
		t.Fatal("nil context was accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := resolver.Resolve(ctx, fileIdentifier(unsupported)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func TestDefaultFileResolverOptionsRequireExplicitRoots(t *testing.T) {
	t.Parallel()

	options := reference.DefaultFileResolverOptions()
	if len(options.AllowedRoots) != 0 || options.MaxBytes != 16_777_216 ||
		options.MaxDocuments < 1 {
		t.Fatalf("default options = %#v", options)
	}
	root := t.TempDir()
	path := writeResolverFixture(t, root, "schema.json", `{}`)
	options.AllowedRoots = []string{root}
	resolver, err := reference.NewFileResolver(options)
	if err != nil {
		t.Fatal(err)
	}
	if err := resolver.Close(); err != nil {
		t.Fatal(err)
	}
	if err := resolver.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if _, err := resolver.Resolve(
		context.Background(), fileIdentifier(path),
	); !errors.Is(err, reference.ErrResourceDenied) {
		t.Fatalf("closed resolver error = %v", err)
	}
}

func TestFileResolverDeduplicatesConfiguredRoots(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := writeResolverFixture(t, root, "schema.json", `{}`)
	options := reference.DefaultFileResolverOptions()
	options.AllowedRoots = []string{root, root}
	options.MaxDocuments = 1
	resolver, err := reference.NewFileResolver(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := resolver.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := resolver.Resolve(
		context.Background(), fileIdentifier(path),
	); err != nil {
		t.Fatal(err)
	}
}

func mustFileResolver(t *testing.T, root string, maxDocuments int) *reference.FileResolver {
	t.Helper()
	resolver, err := reference.NewFileResolver(reference.FileResolverOptions{
		AllowedRoots: []string{root},
		MaxBytes:     1024,
		MaxDocuments: maxDocuments,
		ParseLimits:  parse.DefaultLimits(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := resolver.Close(); closeErr != nil {
			t.Errorf("close file resolver: %v", closeErr)
		}
	})
	return resolver
}

func writeResolverFixture(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func fileIdentifier(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func TestFileResolverErrorsDoNotExposeResourceContents(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := writeResolverFixture(t, root, "broken.json", `{"secret":"token"`)
	resolver := mustFileResolver(t, root, 1)
	_, err := resolver.Resolve(context.Background(), fileIdentifier(path))
	if err == nil {
		t.Fatal("malformed resource was accepted")
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "token") {
		t.Fatalf("error exposed resource contents: %v", err)
	}
}

func TestFileResolverAccessErrorsDoNotExposeResourcePaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	resolver := mustFileResolver(t, root, 1)
	path := filepath.Join(root, "private-customer-token.json")
	_, err := resolver.Resolve(context.Background(), fileIdentifier(path))
	if !errors.Is(err, reference.ErrResourceAccess) {
		t.Fatalf("missing resource error = %v, want access failure", err)
	}
	if strings.Contains(err.Error(), "private-customer-token") ||
		strings.Contains(err.Error(), root) {
		t.Fatalf("error exposed resource path: %v", err)
	}
}
