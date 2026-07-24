package reference

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/parse"
)

type failingFileRoot struct {
	file     *os.File
	openErr  error
	closeErr error
}

func (root *failingFileRoot) Open(string) (*os.File, error) {
	return root.file, root.openErr
}

func (root *failingFileRoot) Close() error { return root.closeErr }

func TestFileResolverInjectsConstructorAndCleanupFailures(t *testing.T) {
	t.Parallel()

	want := errors.New("open root")
	options := DefaultFileResolverOptions()
	options.AllowedRoots = []string{t.TempDir()}
	if _, err := newFileResolver(
		options,
		canonicalDirectory,
		func(string) (fileRootHandle, error) { return nil, want },
	); !errors.Is(err, ErrResourceAccess) || strings.Contains(err.Error(), want.Error()) {
		t.Fatalf("open root error = %v", err)
	}

	want = errors.New("close root")
	if err := closeFileRoots([]fileRoot{{
		handle: &failingFileRoot{closeErr: want},
	}}); !errors.Is(err, ErrResourceAccess) || strings.Contains(err.Error(), want.Error()) {
		t.Fatalf("close root error = %v", err)
	}
}

func TestFileResolverInjectsOpenAndStatFailures(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "resource.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("private backend detail")
	resolver := &FileResolver{
		roots: []fileRoot{{
			path: canonicalRoot, handle: &failingFileRoot{openErr: want},
		}},
		maxDocuments: 1,
		parseLimits:  parse.DefaultLimits(),
	}
	if _, err := resolver.Resolve(
		context.Background(), fileIdentifierInternal(path),
	); !errors.Is(err, ErrResourceAccess) || strings.Contains(err.Error(), want.Error()) {
		t.Fatalf("open resource error = %v", err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	resolver = &FileResolver{
		roots: []fileRoot{{
			path: canonicalRoot, handle: &failingFileRoot{file: file},
		}},
		maxDocuments: 1,
		parseLimits:  parse.DefaultLimits(),
	}
	if _, err := resolver.Resolve(
		context.Background(), fileIdentifierInternal(path),
	); !errors.Is(err, ErrResourceAccess) || strings.Contains(err.Error(), path) {
		t.Fatalf("closed resource stat error = %v", err)
	}
}

func TestCanonicalDirectoryInjectsPathFailures(t *testing.T) {
	t.Parallel()

	want := errors.New("path failure")
	identity := func(value string) (string, error) { return value, nil }
	stat := func(string) (os.FileInfo, error) { return nil, want }
	if _, err := canonicalDirectoryWith(
		"root", func(string) (string, error) { return "", want }, identity, stat,
	); !errors.Is(err, ErrResourceAccess) || strings.Contains(err.Error(), want.Error()) {
		t.Fatalf("absolute path error = %v", err)
	}
	if _, err := canonicalDirectoryWith(
		"root", identity, identity, stat,
	); !errors.Is(err, ErrResourceAccess) || strings.Contains(err.Error(), want.Error()) {
		t.Fatalf("stat path error = %v", err)
	}
}

func TestValidParseLimitsAcceptsEveryMinimum(t *testing.T) {
	t.Parallel()

	limits := parse.Limits{
		MaxBytes: 1, MaxTokens: 1, MaxDepth: 1, MaxObjectMembers: 1,
		MaxArrayItems: 1, MaxScalarBytes: 1, MaxTotalValues: 1,
	}
	if err := validParseLimits(limits); err != nil {
		t.Fatalf("minimum parse limits error = %v", err)
	}
}

func fileIdentifierInternal(path string) string {
	return "file://" + filepath.ToSlash(path)
}
