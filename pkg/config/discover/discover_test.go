package discover_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/config/discover"
)

func TestSearchDoesNotTraverseParentsByDefault(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	start := filepath.Join(root, "one", "two")
	mustMkdirAll(t, start)
	mustWrite(t, filepath.Join(root, "app.yaml"), "name: parent")

	results, err := discover.Search(context.Background(), discover.Options{
		StartDir: start, Root: root, SearchPlaces: []string{"app.yaml"},
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("Search() results = %#v, want none", results)
	}
}

func TestSearchUpwardStopsAtExplicitDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	start := filepath.Join(root, "one", "two")
	mustMkdirAll(t, start)
	mustWrite(t, filepath.Join(root, "one", "app.yaml"), "name: one")
	mustWrite(t, filepath.Join(root, "app.yaml"), "name: root")

	results, err := discover.Search(context.Background(), discover.Options{
		StartDir: start, StopDir: root, Root: root, Upward: true,
		SearchPlaces: []string{"app.yaml"}, Mode: discover.SearchAll,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	want := []string{
		filepath.Join(root, "one", "app.yaml"),
		filepath.Join(root, "app.yaml"),
	}
	if got := paths(results); !reflect.DeepEqual(got, want) {
		t.Fatalf("Search() paths = %#v, want %#v", got, want)
	}
	if results[0].SearchPlace != "app.yaml" || results[0].Directory != filepath.Join(root, "one") {
		t.Fatalf("Search() provenance = %#v", results[0])
	}
}

func TestSearchHonorsDirectoryAndPlaceOrder(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	mustMkdirAll(t, first)
	mustMkdirAll(t, second)
	mustWrite(t, filepath.Join(first, "app.json"), "{}")
	mustWrite(t, filepath.Join(first, "app.yaml"), "{}")
	mustWrite(t, filepath.Join(second, "app.json"), "{}")

	results, err := discover.Search(context.Background(), discover.Options{
		Root: root, Directories: []string{second, first},
		SearchPlaces: []string{"app.yaml", "app.json"}, Mode: discover.SearchAll,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	want := []string{
		filepath.Join(second, "app.json"),
		filepath.Join(first, "app.yaml"),
		filepath.Join(first, "app.json"),
	}
	if got := paths(results); !reflect.DeepEqual(got, want) {
		t.Fatalf("Search() paths = %#v, want %#v", got, want)
	}

	firstOnly, err := discover.Search(context.Background(), discover.Options{
		Root: root, Directories: []string{second, first},
		SearchPlaces: []string{"app.yaml", "app.json"}, Mode: discover.SearchFirst,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if got := paths(firstOnly); !reflect.DeepEqual(got, want[:1]) {
		t.Fatalf("SearchFirst paths = %#v, want %#v", got, want[:1])
	}
}

func TestSearchAcceptsExplicitFilesAndDirectories(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	directory := filepath.Join(root, "config")
	mustMkdirAll(t, directory)
	explicit := filepath.Join(root, "explicit.toml")
	mustWrite(t, explicit, "name = 'explicit'")
	mustWrite(t, filepath.Join(directory, "app.yaml"), "name: directory")

	results, err := discover.Search(context.Background(), discover.Options{
		Root: root, Explicit: []string{explicit, directory},
		SearchPlaces: []string{"app.yaml"}, Mode: discover.SearchAll,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	want := []string{explicit, filepath.Join(directory, "app.yaml")}
	if got := paths(results); !reflect.DeepEqual(got, want) {
		t.Fatalf("Search() paths = %#v, want %#v", got, want)
	}
	if results[0].Explicit != true || results[1].Explicit != true {
		t.Fatalf("Search() explicit provenance = %#v", results)
	}
}

func TestSearchUserConfigDirectoryIsExplicit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	userConfig := filepath.Join(root, "user")
	applicationDir := filepath.Join(userConfig, "myapp")
	mustMkdirAll(t, applicationDir)
	mustWrite(t, filepath.Join(applicationDir, "config.yaml"), "name: user")

	without, err := discover.Search(context.Background(), discover.Options{
		Root: root, SearchPlaces: []string{"config.yaml"},
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(without) != 0 {
		t.Fatalf("Search() implicit user results = %#v", without)
	}

	with, err := discover.Search(context.Background(), discover.Options{
		Root: root, SearchPlaces: []string{"config.yaml"},
		UseUserConfigDir: true, UserConfigDir: userConfig, Application: "myapp",
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	want := filepath.Join(applicationDir, "config.yaml")
	if len(with) != 1 || with[0].Path != want || !with[0].UserConfig {
		t.Fatalf("Search() user result = %#v", with)
	}
}

func TestSearchEnforcesSymlinkAndRootPolicies(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink setup requires Windows privileges")
	}
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	mustWrite(t, filepath.Join(root, "real.yaml"), "name: real")
	mustWrite(t, filepath.Join(outside, "outside.yaml"), "name: outside")
	insideLink := filepath.Join(root, "inside.yaml")
	outsideLink := filepath.Join(root, "outside.yaml")
	if err := os.Symlink(filepath.Join(root, "real.yaml"), insideLink); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "outside.yaml"), outsideLink); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	_, err := discover.Search(context.Background(), discover.Options{
		Root: root, Explicit: []string{insideLink},
	})
	if !errors.Is(err, discover.ErrSymlink) {
		t.Fatalf("Search() error = %v, want ErrSymlink", err)
	}
	if strings.Contains(err.Error(), insideLink) {
		t.Fatalf("Search() symlink error exposed rejected path: %v", err)
	}

	allowed, err := discover.Search(context.Background(), discover.Options{
		Root: root, Explicit: []string{insideLink}, Symlinks: discover.AllowWithinRoot,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	resolvedReal, err := filepath.EvalSymlinks(filepath.Join(root, "real.yaml"))
	if err != nil {
		t.Fatalf("EvalSymlinks() error = %v", err)
	}
	if len(allowed) != 1 || !allowed[0].Symlink || allowed[0].ResolvedPath != resolvedReal {
		t.Fatalf("Search() allowed symlink = %#v", allowed)
	}

	_, err = discover.Search(context.Background(), discover.Options{
		Root: root, Explicit: []string{outsideLink}, Symlinks: discover.AllowWithinRoot,
	})
	if !errors.Is(err, discover.ErrOutsideRoot) {
		t.Fatalf("Search() error = %v, want ErrOutsideRoot", err)
	}
	if strings.Contains(err.Error(), outside) {
		t.Fatalf("Search() root error exposed rejected path: %v", err)
	}
}

func TestSearchChecksSecretFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions are not available")
	}
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "secret.yaml")
	mustWrite(t, path, "token: secret")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	_, err := discover.Search(context.Background(), discover.Options{
		Root: root, Explicit: []string{path}, Permissions: discover.OwnerOnly,
	})
	if !errors.Is(err, discover.ErrPermissions) {
		t.Fatalf("Search() error = %v, want ErrPermissions", err)
	}
	if strings.Contains(err.Error(), path) {
		t.Fatalf("Search() permission error exposed rejected path: %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	if _, err := discover.Search(context.Background(), discover.Options{
		Root: root, Explicit: []string{path}, Permissions: discover.OwnerOnly,
	}); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
}

func TestSearchRejectsUnsafeOrUnboundedOptions(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	start := filepath.Join(root, "nested")
	mustMkdirAll(t, start)
	tests := map[string]discover.Options{
		"upward without stop": {Root: root, StartDir: start, Upward: true},
		"stop outside root":   {Root: root, StartDir: start, StopDir: t.TempDir(), Upward: true},
		"traversal place":     {Root: root, StartDir: start, SearchPlaces: []string{"../app.yaml"}},
		"negative candidates": {Root: root, MaxCandidates: -1},
		"negative results":    {Root: root, MaxResults: -1},
	}
	for name, options := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := discover.Search(context.Background(), options); err == nil {
				t.Fatal("Search() error = nil, want validation error")
			}
		})
	}
}

func TestSearchHonorsCancellationAndCandidateLimit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := discover.Search(ctx, discover.Options{Root: root}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Search() error = %v, want context.Canceled", err)
	}

	_, err := discover.Search(context.Background(), discover.Options{
		Root: root, Directories: []string{root},
		SearchPlaces: []string{"one", "two"}, MaxCandidates: 1,
	})
	if !errors.Is(err, discover.ErrLimit) {
		t.Fatalf("Search() error = %v, want ErrLimit", err)
	}
}

func paths(results []discover.Result) []string {
	paths := make([]string, len(results))
	for index, result := range results {
		paths[index] = result.Path
	}
	return paths
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
