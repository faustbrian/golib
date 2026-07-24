package discover

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestValidateRejectsEveryPolicyAndPathCategory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	inside := filepath.Join(root, "inside")
	if err := os.Mkdir(inside, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	outside := t.TempDir()
	tests := map[string]Options{
		"mode":                  {Mode: SearchAll + 1},
		"symlinks":              {Symlinks: AllowWithinRoot + 1},
		"permissions":           {Permissions: OwnerOnly + 1},
		"negative upward depth": {MaxUpwardDepth: -1},
		"empty place":           {SearchPlaces: []string{""}},
		"dot place":             {SearchPlaces: []string{"."}},
		"parent place":          {SearchPlaces: []string{".."}},
		"nested place":          {SearchPlaces: []string{"nested/config.yaml"}},
		"upward missing start":  {Upward: true, StopDir: root},
		"unsafe application": {
			UseUserConfigDir: true, UserConfigDir: root, Application: "../app",
		},
		"missing root": {Root: filepath.Join(root, "missing")},
		"start outside root": {
			Root: root, StartDir: outside,
		},
		"stop outside root": {
			Root: root, StartDir: inside, StopDir: outside,
		},
		"stop does not contain start": {
			StartDir: inside, StopDir: outside, Upward: true,
		},
		"user directory outside root": {
			Root: root, UseUserConfigDir: true, UserConfigDir: outside, Application: "app",
		},
	}
	for name, options := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := validate(options); err == nil {
				t.Fatal("validate() error = nil")
			}
		})
	}
}

func TestValidatePropagatesInjectedPlatformFailures(t *testing.T) {
	t.Parallel()

	failure := errors.New("platform failure")
	tests := map[string]struct {
		options Options
		mutate  func(*fileOperations)
	}{
		"user config": {
			options: Options{UseUserConfigDir: true, Application: "app"},
			mutate: func(operations *fileOperations) {
				operations.userConfigDir = func() (string, error) { return "", failure }
			},
		},
		"root absolute": {
			options: Options{Root: "root"},
			mutate: func(operations *fileOperations) {
				operations.absolute = func(string) (string, error) { return "", failure }
			},
		},
		"start resolved": {
			options: Options{StartDir: "start"},
			mutate: func(operations *fileOperations) {
				operations.evalSymlinks = func(string) (string, error) { return "", failure }
			},
		},
		"stop resolved": {
			options: Options{StopDir: "stop"},
			mutate: func(operations *fileOperations) {
				operations.evalSymlinks = func(string) (string, error) { return "", failure }
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			operations := defaultFileOperations
			test.mutate(&operations)
			if _, err := validateWith(test.options, operations); !errors.Is(err, failure) {
				t.Fatalf("validateWith() error = %v, want platform failure", err)
			}
		})
	}
}

func TestDiscoveryPlatformFailuresPreserveIdentityWithoutLeakingText(t *testing.T) {
	t.Parallel()

	canary := errors.New("canary-secret-platform-path")
	operations := defaultFileOperations
	operations.absolute = func(string) (string, error) { return "", canary }
	_, err := search(context.Background(), Options{Explicit: []string{"config.yaml"}}, operations)
	if !errors.Is(err, canary) {
		t.Fatalf("search() error = %v, want preserved identity", err)
	}
	if strings.Contains(err.Error(), canary.Error()) {
		t.Fatalf("search() error leaked platform text: %v", err)
	}
}

func TestValidateUsesDefaultUserConfigurationDirectory(t *testing.T) {
	configured, err := validate(Options{
		UseUserConfigDir: true,
		Application:      "app",
	})
	if err != nil {
		t.Fatalf("validate() error = %v", err)
	}
	if configured.UserConfigDir == "" || !filepath.IsAbs(configured.UserConfigDir) {
		t.Fatalf("UserConfigDir = %q", configured.UserConfigDir)
	}
}

func TestAbsoluteJoinsRootAndRejectsEscapes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	configured, err := validate(Options{Root: root})
	if err != nil {
		t.Fatalf("validate() error = %v", err)
	}
	got, err := configured.absolute("nested/config.yaml")
	if err != nil {
		t.Fatalf("absolute() error = %v", err)
	}
	want := filepath.Join(root, "nested", "config.yaml")
	if got != want {
		t.Fatalf("absolute() = %q, want %q", got, want)
	}
	rejected := filepath.Join(root, "..", "outside")
	if _, err := configured.absolute(rejected); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("absolute() error = %v, want ErrOutsideRoot", err)
	} else if strings.Contains(err.Error(), filepath.Clean(rejected)) {
		t.Fatalf("absolute() error exposed rejected path: %v", err)
	}
}

func TestSearchHandlesDuplicateMissingAndBoundedResults(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	first := filepath.Join(root, "first.yaml")
	second := filepath.Join(root, "second.yaml")
	mustWriteDiscoveryFile(t, first)
	mustWriteDiscoveryFile(t, second)

	results, err := Search(context.Background(), Options{
		Root: root,
		Explicit: []string{
			filepath.Join(root, "missing.yaml"), first, first,
		},
		Mode: SearchAll,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].Path != first {
		t.Fatalf("Search() results = %#v", results)
	}

	_, err = Search(context.Background(), Options{
		Root: root, Explicit: []string{first, second},
		Mode: SearchAll, MaxResults: 1,
	})
	if !errors.Is(err, ErrLimit) {
		t.Fatalf("Search() error = %v, want ErrLimit", err)
	}
}

func TestSearchRejectsExplicitAndDirectoryPathsOutsideRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	for name, options := range map[string]Options{
		"explicit":  {Root: root, Explicit: []string{outside}},
		"directory": {Root: root, Directories: []string{outside}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := Search(context.Background(), options); !errors.Is(err, ErrOutsideRoot) {
				t.Fatalf("Search() error = %v, want ErrOutsideRoot", err)
			}
		})
	}
}

func TestSearchRejectsSymlinkLoopsWithoutExposingPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink setup requires Windows privileges")
	}
	t.Parallel()

	root := t.TempDir()
	first := filepath.Join(root, "first.yaml")
	second := filepath.Join(root, "second.yaml")
	if err := os.Symlink(second, first); err != nil {
		t.Fatalf("Symlink(first) error = %v", err)
	}
	if err := os.Symlink(first, second); err != nil {
		t.Fatalf("Symlink(second) error = %v", err)
	}
	_, err := Search(context.Background(), Options{
		Root: root, Explicit: []string{first}, Symlinks: AllowWithinRoot,
	})
	if err == nil {
		t.Fatal("Search() error = nil, want symlink loop failure")
	}
	if strings.Contains(err.Error(), first) || strings.Contains(err.Error(), second) {
		t.Fatalf("Search() error exposed loop path: %v", err)
	}
}

func TestSearchFollowsHostFilesystemPathCasing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	actual := filepath.Join(root, "Config.yaml")
	mustWriteDiscoveryFile(t, actual)
	alternateName := "config.yaml"
	_, statErr := os.Stat(filepath.Join(root, alternateName))
	results, err := Search(context.Background(), Options{
		Root: root, Directories: []string{root}, SearchPlaces: []string{alternateName},
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if statErr == nil && len(results) != 1 {
		t.Fatalf("case-insensitive host results = %#v, want one", results)
	}
	if errors.Is(statErr, os.ErrNotExist) && len(results) != 0 {
		t.Fatalf("case-sensitive host results = %#v, want none", results)
	}
}

func TestSearchStopsAtCanonicalCaseVariantDirectory(t *testing.T) {
	t.Parallel()

	base := filepath.Join(t.TempDir(), "base")
	stop := filepath.Join(base, "stop")
	start := filepath.Join(stop, "child")
	requestedStop := strings.ToUpper(stop)
	var candidates []string
	operations := defaultFileOperations
	operations.absolute = func(path string) (string, error) {
		return filepath.Clean(path), nil
	}
	operations.evalSymlinks = func(path string) (string, error) {
		if strings.EqualFold(path, stop) {
			return stop, nil
		}
		return filepath.Clean(path), nil
	}
	operations.lstat = func(path string) (os.FileInfo, error) {
		candidates = append(candidates, path)
		return nil, fs.ErrNotExist
	}
	operations.relative = func(parent, child string) (string, error) {
		if strings.EqualFold(parent, stop) && strings.HasPrefix(
			strings.ToLower(child),
			strings.ToLower(stop)+string(filepath.Separator),
		) {
			return "child", nil
		}
		return filepath.Rel(parent, child)
	}
	results, err := search(context.Background(), Options{
		StartDir: start, StopDir: requestedStop, Upward: true,
		SearchPlaces: []string{"config.yaml"}, MaxUpwardDepth: 4,
	}, operations)
	if err != nil {
		t.Fatalf("search() error = %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("search() results = %#v, want none", results)
	}
	want := []string{
		filepath.Join(start, "config.yaml"),
		filepath.Join(stop, "config.yaml"),
	}
	if !reflect.DeepEqual(candidates, want) {
		t.Fatalf("candidate paths = %#v, want %#v", candidates, want)
	}
}

func TestSearchPropagatesUpwardCanonicalizationFailure(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	start := filepath.Join(root, "child")
	if err := os.Mkdir(start, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	failure := errors.New("canonicalization failure")
	operations := defaultFileOperations
	evaluations := 0
	operations.evalSymlinks = func(path string) (string, error) {
		evaluations++
		if evaluations > 2 {
			return "", failure
		}
		return filepath.Clean(path), nil
	}
	_, err := search(context.Background(), Options{
		StartDir: start, StopDir: root, Upward: true,
		SearchPlaces: []string{"missing.yaml"},
	}, operations)
	if !errors.Is(err, failure) {
		t.Fatalf("search() error = %v, want canonicalization failure", err)
	}
}

func TestInspectUsesPathComponentsForSymlinkDetection(t *testing.T) {
	t.Parallel()

	regularInfo, err := os.Stat(os.Args[0])
	if err != nil {
		t.Fatalf("Stat(test binary) error = %v", err)
	}
	root := filepath.Join(t.TempDir(), "root")
	failure := errors.New("ancestor metadata failure")
	tests := map[string]struct {
		path           string
		resolved       string
		symlinkDir     string
		lstatErrorDir  string
		configuredRoot string
		relativeResult string
		rootless       bool
		wantError      error
	}{
		"case normalization is not a symlink": {
			path:     filepath.Join(root, "config.yaml"),
			resolved: filepath.Join(root, "Config.yaml"),
		},
		"case distinct symlink component is rejected": {
			path:       filepath.Join(root, "link", "config.yaml"),
			resolved:   filepath.Join(root, "LINK", "config.yaml"),
			symlinkDir: filepath.Join(root, "link"),
			wantError:  ErrSymlink,
		},
		"ancestor metadata failure is propagated": {
			path:          filepath.Join(root, "unreadable", "config.yaml"),
			resolved:      filepath.Join(root, "unreadable", "config.yaml"),
			lstatErrorDir: filepath.Join(root, "unreadable"),
			wantError:     failure,
		},
		"rootless walk terminates at filesystem root": {
			path:     filepath.Join(string(filepath.Separator), "rootless", "config.yaml"),
			resolved: filepath.Join(string(filepath.Separator), "rootless", "config.yaml"),
			rootless: true,
		},
		"case variant root identity stops component walk": {
			path:           filepath.Join(root, "config.yaml"),
			resolved:       filepath.Join(root, "config.yaml"),
			configuredRoot: strings.ToUpper(root),
			lstatErrorDir:  filepath.Dir(root),
		},
		"root metadata failure is propagated": {
			path:           filepath.Join(root, "config.yaml"),
			resolved:       filepath.Join(root, "config.yaml"),
			configuredRoot: strings.ToUpper(root),
			lstatErrorDir:  strings.ToUpper(root),
			wantError:      failure,
		},
		"case distinct sibling root is rejected": {
			path:           filepath.Join(root, "config.yaml"),
			resolved:       filepath.Join(strings.ToUpper(root), "config.yaml"),
			relativeResult: "config.yaml",
			wantError:      ErrOutsideRoot,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			operations := defaultFileOperations
			operations.evalSymlinks = func(string) (string, error) {
				return test.resolved, nil
			}
			operations.lstat = func(path string) (os.FileInfo, error) {
				if path == test.lstatErrorDir {
					return nil, failure
				}
				identity := filepath.Clean(path)
				if test.configuredRoot != "" &&
					(path == test.configuredRoot || path == root) {
					identity = "configured-root"
				}
				mode := regularInfo.Mode()
				if path == test.symlinkDir {
					mode = os.ModeSymlink
				}
				return fileInfoWithMode{
					FileInfo: regularInfo, mode: mode, identity: identity,
				}, nil
			}
			operations.sameFile = func(left, right os.FileInfo) bool {
				return left.Name() == right.Name()
			}
			operations.stat = func(string) (os.FileInfo, error) {
				return regularInfo, nil
			}
			if test.relativeResult != "" {
				operations.relative = func(string, string) (string, error) {
					return test.relativeResult, nil
				}
			}
			configured := settings{operations: operations}
			if !test.rootless {
				configured.Root = root
				configured.rootAbs = test.configuredRoot
				if configured.rootAbs == "" {
					configured.rootAbs = root
				}
				configured.rootResolved = root
			}
			result, found, err := configured.inspect(test.path, candidate{})
			if !errors.Is(err, test.wantError) {
				t.Fatalf("inspect() error = %v, want %v", err, test.wantError)
			}
			if test.wantError == nil && (!found || result.Symlink) {
				t.Fatalf("inspect() = %#v, found %v; want non-symlink result", result, found)
			}
		})
	}
}

func TestSearchPropagatesInjectedCandidateAndExplicitStatFailures(t *testing.T) {
	t.Parallel()

	failure := errors.New("platform failure")
	t.Run("candidate absolute", func(t *testing.T) {
		t.Parallel()
		operations := defaultFileOperations
		operations.absolute = func(path string) (string, error) {
			if strings.HasSuffix(path, "config.yaml") {
				return "", failure
			}
			return filepath.Abs(path)
		}
		_, err := search(context.Background(), Options{
			Directories: []string{t.TempDir()}, SearchPlaces: []string{"config.yaml"},
		}, operations)
		if !errors.Is(err, failure) {
			t.Fatalf("search() error = %v, want platform failure", err)
		}
	})
	t.Run("explicit lstat", func(t *testing.T) {
		t.Parallel()
		operations := defaultFileOperations
		operations.lstat = func(string) (os.FileInfo, error) { return nil, failure }
		_, err := search(context.Background(), Options{Explicit: []string{"config.yaml"}}, operations)
		if !errors.Is(err, failure) {
			t.Fatalf("search() error = %v, want platform failure", err)
		}
	})
}

func TestSearchPropagatesVisitErrorsFromEveryTraversalKind(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink setup requires Windows privileges")
	}
	t.Parallel()

	root := t.TempDir()
	brokenName := "broken.yaml"
	makeBroken := func(t *testing.T, directory string) {
		t.Helper()
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatalf("MkdirAll() error = %v", err)
		}
		if err := os.Symlink(filepath.Join(root, "absent"), filepath.Join(directory, brokenName)); err != nil {
			t.Fatalf("Symlink() error = %v", err)
		}
	}
	explicitDirectory := filepath.Join(root, "explicit")
	upwardDirectory := filepath.Join(root, "upward")
	userDirectory := filepath.Join(root, "user", "app")
	makeBroken(t, explicitDirectory)
	makeBroken(t, upwardDirectory)
	makeBroken(t, userDirectory)

	tests := map[string]Options{
		"explicit": {
			Root: root, Explicit: []string{explicitDirectory}, SearchPlaces: []string{brokenName},
		},
		"upward": {
			Root: root, StartDir: upwardDirectory, StopDir: root, Upward: true,
			SearchPlaces: []string{brokenName},
		},
		"user": {
			Root: root, UseUserConfigDir: true, UserConfigDir: filepath.Join(root, "user"),
			Application: "app", SearchPlaces: []string{brokenName},
		},
	}
	for name, options := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := Search(context.Background(), options); err == nil {
				t.Fatal("Search() error = nil")
			}
		})
	}
}

func TestSearchStopsInsideExplicitDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteDiscoveryFile(t, filepath.Join(root, "config.yaml"))
	results, err := Search(context.Background(), Options{
		Root: root, Explicit: []string{root},
		SearchPlaces: []string{"config.yaml"}, Mode: SearchFirst,
	})
	if err != nil || len(results) != 1 {
		t.Fatalf("Search() = %#v, %v", results, err)
	}
}

func TestSearchEnforcesUpwardDepthAndCanStopAtStart(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	start := filepath.Join(root, "nested")
	if err := os.Mkdir(start, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	_, err := Search(context.Background(), Options{
		Root: root, StartDir: start, StopDir: root, Upward: true,
		SearchPlaces: []string{"missing.yaml"}, MaxUpwardDepth: 1,
	})
	if !errors.Is(err, ErrLimit) {
		t.Fatalf("Search() error = %v, want ErrLimit", err)
	}

	mustWriteDiscoveryFile(t, filepath.Join(start, "config.yaml"))
	results, err := Search(context.Background(), Options{
		Root: root, StartDir: start, StopDir: root, Upward: true,
		SearchPlaces: []string{"config.yaml"}, Mode: SearchFirst,
	})
	if err != nil || len(results) != 1 {
		t.Fatalf("Search() = %#v, %v", results, err)
	}
}

func TestSearchPropagatesCancellationDuringCandidateConsideration(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	_, err := Search(&stagedDiscoveryContext{}, Options{
		Root: root, Directories: []string{root}, SearchPlaces: []string{"config.yaml"},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Search() error = %v, want context.Canceled", err)
	}
}

func TestInspectHandlesMissingDirectoryBrokenLinkAndNonRegularFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix socket and symlink setup requires Unix")
	}
	t.Parallel()

	root := t.TempDir()
	configured, err := validate(Options{Root: root, Symlinks: AllowWithinRoot})
	if err != nil {
		t.Fatalf("validate() error = %v", err)
	}
	for name, path := range map[string]string{
		"missing":   filepath.Join(root, "missing"),
		"directory": root,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, found, err := configured.inspect(path, candidate{}); err != nil || found {
				t.Fatalf("inspect() = found %v, error %v", found, err)
			}
		})
	}

	broken := filepath.Join(root, "broken")
	if err := os.Symlink(filepath.Join(root, "absent"), broken); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	if _, _, err := configured.inspect(broken, candidate{}); err == nil {
		t.Fatal("inspect(broken link) error = nil")
	}

	socketDirectory, err := os.MkdirTemp("/tmp", "config-discover-")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDirectory) })
	socket := filepath.Join(socketDirectory, "config.sock")
	configuredWithoutRoot, err := validate(Options{Symlinks: AllowWithinRoot})
	if err != nil {
		t.Fatalf("validate() error = %v", err)
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if _, found, err := configuredWithoutRoot.inspect(socket, candidate{}); err != nil || found {
		t.Fatalf("inspect(socket) = found %v, error %v", found, err)
	}
}

func TestWithinRejectsParentAndAcceptsSameOrChild(t *testing.T) {
	t.Parallel()

	root := filepath.Clean("/root")
	if !within(root, root) || !within(root, filepath.Join(root, "child")) {
		t.Fatal("within() rejected same or child path")
	}
	if within(root, filepath.Clean("/outside")) {
		t.Fatal("within() accepted outside path")
	}
	if withinWith(root, root, func(string, string) (string, error) {
		return "", errors.New("relative failure")
	}) {
		t.Fatal("withinWith() accepted relative-path failure")
	}
}

func TestAbsoluteAndInspectPropagateInjectedPlatformFailures(t *testing.T) {
	t.Parallel()

	failure := errors.New("platform failure")
	operations := defaultFileOperations
	operations.absolute = func(string) (string, error) { return "", failure }
	configured := settings{operations: operations}
	if _, err := configured.absolute("config.yaml"); !errors.Is(err, failure) {
		t.Fatalf("absolute() error = %v, want platform failure", err)
	}

	caseFoldingOperations := defaultFileOperations
	caseFoldingOperations.absolute = func(path string) (string, error) {
		return filepath.Clean(path), nil
	}
	caseFoldingOperations.relative = func(string, string) (string, error) {
		return "config.yaml", nil
	}
	volumeRoot := filepath.VolumeName(os.Args[0]) + string(filepath.Separator)
	configured = settings{
		operations: caseFoldingOperations,
		rootAbs:    filepath.Join(volumeRoot, "root"),
	}
	caseDistinctSibling := filepath.Join(volumeRoot, "ROOT", "config.yaml")
	if _, err := configured.absolute(caseDistinctSibling); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("absolute(case-distinct sibling) error = %v, want %v", err, ErrOutsideRoot)
	}
	caseFoldingOperations.relative = func(string, string) (string, error) {
		return "", failure
	}
	configured.operations = caseFoldingOperations
	if _, err := configured.absolute(filepath.Join(configured.rootAbs, "config.yaml")); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("absolute(relative failure) error = %v, want %v", err, ErrOutsideRoot)
	}

	regularInfo, err := os.Stat(os.Args[0])
	if err != nil {
		t.Fatalf("Stat(test binary) error = %v", err)
	}
	tests := map[string]func(*fileOperations){
		"lstat": func(operations *fileOperations) {
			operations.lstat = func(string) (os.FileInfo, error) { return nil, failure }
		},
		"relative": func(operations *fileOperations) {
			operations.lstat = func(string) (os.FileInfo, error) { return regularInfo, nil }
			operations.evalSymlinks = func(path string) (string, error) { return path, nil }
			operations.relative = func(string, string) (string, error) { return "", failure }
		},
		"stat": func(operations *fileOperations) {
			operations.lstat = func(string) (os.FileInfo, error) { return regularInfo, nil }
			operations.evalSymlinks = func(path string) (string, error) { return path, nil }
			operations.stat = func(string) (os.FileInfo, error) { return nil, failure }
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			operations := defaultFileOperations
			mutate(&operations)
			configured := settings{
				Options: Options{Root: "/root"}, rootAbs: "/root", rootResolved: "/root",
				operations: operations,
			}
			if _, _, err := configured.inspect("/root/config.yaml", candidate{}); !errors.Is(err, failure) {
				t.Fatalf("inspect() error = %v, want platform failure", err)
			}
		})
	}
}

func mustWriteDiscoveryFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("value"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

type stagedDiscoveryContext struct{ calls int }

type fileInfoWithMode struct {
	os.FileInfo
	mode     os.FileMode
	identity string
}

func (info fileInfoWithMode) Mode() os.FileMode { return info.mode }
func (info fileInfoWithMode) Name() string {
	if info.identity != "" {
		return info.identity
	}
	return info.FileInfo.Name()
}

func (*stagedDiscoveryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*stagedDiscoveryContext) Done() <-chan struct{}       { return nil }
func (*stagedDiscoveryContext) Value(any) any               { return nil }
func (c *stagedDiscoveryContext) Err() error {
	c.calls++
	if c.calls > 1 {
		return context.Canceled
	}
	return nil
}
