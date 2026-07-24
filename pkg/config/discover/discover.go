// Package discover provides explicit, bounded configuration file discovery.
package discover

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/faustbrian/golib/pkg/config/internal/safeerror"
)

const (
	defaultMaxCandidates  = 1_000
	defaultMaxResults     = 100
	defaultMaxUpwardDepth = 64
)

var (
	// ErrSymlink indicates that a candidate traverses a symlink under a reject policy.
	ErrSymlink = errors.New("configuration discovery symlink rejected")
	// ErrOutsideRoot indicates that a lexical or resolved path escapes Root.
	ErrOutsideRoot = errors.New("configuration discovery path outside root")
	// ErrPermissions indicates that a file violates the requested mode policy.
	ErrPermissions = errors.New("configuration discovery file permissions rejected")
	// ErrLimit indicates that a candidate, result, or traversal bound was exceeded.
	ErrLimit = errors.New("configuration discovery limit exceeded")
)

// Mode controls whether discovery stops at the first result.
type Mode uint8

const (
	SearchFirst Mode = iota
	SearchAll
)

// SymlinkPolicy controls candidate symlinks.
type SymlinkPolicy uint8

const (
	RejectSymlinks SymlinkPolicy = iota
	AllowWithinRoot
)

// PermissionPolicy controls accepted file modes.
type PermissionPolicy uint8

const (
	IgnorePermissions PermissionPolicy = iota
	OwnerOnly
)

// Options defines all search roots and policies. No path is searched unless it
// is explicitly present here or enabled through UseUserConfigDir.
type Options struct {
	Explicit         []string
	Directories      []string
	StartDir         string
	StopDir          string
	Root             string
	SearchPlaces     []string
	Application      string
	UserConfigDir    string
	UseUserConfigDir bool
	Upward           bool
	Mode             Mode
	Symlinks         SymlinkPolicy
	Permissions      PermissionPolicy
	MaxCandidates    int
	MaxResults       int
	MaxUpwardDepth   int
}

// Result contains discovered-path provenance without file contents.
type Result struct {
	Path         string
	ResolvedPath string
	Directory    string
	SearchPlace  string
	Explicit     bool
	UserConfig   bool
	Symlink      bool
}

type settings struct {
	Options
	operations    fileOperations
	rootAbs       string
	rootResolved  string
	startAbs      string
	startResolved string
	stopAbs       string
	stopResolved  string
}

type fileOperations struct {
	absolute      func(string) (string, error)
	evalSymlinks  func(string) (string, error)
	lstat         func(string) (os.FileInfo, error)
	stat          func(string) (os.FileInfo, error)
	sameFile      func(os.FileInfo, os.FileInfo) bool
	relative      func(string, string) (string, error)
	userConfigDir func() (string, error)
}

var defaultFileOperations = fileOperations{
	absolute: filepath.Abs, evalSymlinks: filepath.EvalSymlinks,
	lstat: os.Lstat, stat: os.Stat, sameFile: os.SameFile, relative: filepath.Rel,
	userConfigDir: os.UserConfigDir,
}

type candidate struct {
	path        string
	directory   string
	searchPlace string
	explicit    bool
	userConfig  bool
}

// Search returns deterministic results according to options.
func Search(ctx context.Context, options Options) ([]Result, error) {
	return search(ctx, options, defaultFileOperations)
}

func search(ctx context.Context, options Options, operations fileOperations) ([]Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	configured, err := validateWith(options, operations)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0)
	seen := make(map[string]struct{})
	candidates := 0
	consider := func(candidate candidate) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		candidates++
		if candidates > configured.MaxCandidates {
			return false, fmt.Errorf("%w: more than %d candidates", ErrLimit, configured.MaxCandidates)
		}
		absolute, err := configured.absolute(candidate.path)
		if err != nil {
			return false, err
		}
		if _, exists := seen[absolute]; exists {
			return false, nil
		}
		seen[absolute] = struct{}{}

		result, found, err := configured.inspect(absolute, candidate)
		if err != nil || !found {
			return false, err
		}
		results = append(results, result)
		if len(results) > configured.MaxResults {
			return false, fmt.Errorf("%w: more than %d results", ErrLimit, configured.MaxResults)
		}
		return configured.Mode == SearchFirst, nil
	}

	for _, explicit := range configured.Explicit {
		absolute, err := configured.absolute(explicit)
		if err != nil {
			return nil, err
		}
		info, err := configured.operations.lstat(absolute)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, discoveryOperationError(err)
		}
		if info.IsDir() {
			stop, err := visitDirectory(absolute, true, false, configured.SearchPlaces, consider)
			if err != nil {
				return nil, err
			}
			if stop {
				return results, nil
			}
			continue
		}
		stop, err := consider(candidate{path: absolute, explicit: true})
		if err != nil {
			return nil, err
		}
		if stop {
			return results, nil
		}
	}

	for _, directory := range configured.Directories {
		absolute, err := configured.absolute(directory)
		if err != nil {
			return nil, err
		}
		stop, err := visitDirectory(absolute, false, false, configured.SearchPlaces, consider)
		if err != nil {
			return nil, err
		}
		if stop {
			return results, nil
		}
	}

	if configured.startAbs != "" {
		directory := configured.startAbs
		for depth := 0; ; depth++ {
			if depth >= configured.MaxUpwardDepth {
				return nil, fmt.Errorf("%w: upward traversal exceeds %d directories", ErrLimit, configured.MaxUpwardDepth)
			}
			stop, err := visitDirectory(directory, false, false, configured.SearchPlaces, consider)
			if err != nil {
				return nil, err
			}
			if stop {
				return results, nil
			}
			if !configured.Upward {
				break
			}
			resolvedDirectory, err := configured.operations.evalSymlinks(directory)
			if err != nil {
				return nil, discoveryOperationError(err)
			}
			if samePath(resolvedDirectory, configured.stopResolved) {
				break
			}
			directory = filepath.Dir(directory)
		}
	}

	if configured.UseUserConfigDir {
		directory := filepath.Join(configured.UserConfigDir, configured.Application)
		stop, err := visitDirectory(directory, false, true, configured.SearchPlaces, consider)
		if err != nil {
			return nil, err
		}
		if stop {
			return results, nil
		}
	}

	return results, nil
}

func validate(options Options) (settings, error) {
	return validateWith(options, defaultFileOperations)
}

func validateWith(options Options, operations fileOperations) (settings, error) {
	if options.Mode > SearchAll || options.Symlinks > AllowWithinRoot || options.Permissions > OwnerOnly {
		return settings{}, errors.New("configuration discovery policy is invalid")
	}
	if options.MaxCandidates < 0 || options.MaxResults < 0 || options.MaxUpwardDepth < 0 {
		return settings{}, errors.New("configuration discovery limits must not be negative")
	}
	if options.MaxCandidates == 0 {
		options.MaxCandidates = defaultMaxCandidates
	}
	if options.MaxResults == 0 {
		options.MaxResults = defaultMaxResults
	}
	if options.MaxUpwardDepth == 0 {
		options.MaxUpwardDepth = defaultMaxUpwardDepth
	}
	for _, place := range options.SearchPlaces {
		if place == "" || filepath.Base(place) != place || place == "." || place == ".." {
			return settings{}, errors.New("configuration search place must be a safe filename")
		}
	}
	if options.Upward && (options.StartDir == "" || options.StopDir == "") {
		return settings{}, errors.New("upward discovery requires start and stop directories")
	}
	if options.UseUserConfigDir {
		if options.Application == "" || filepath.Base(options.Application) != options.Application {
			return settings{}, errors.New("user configuration discovery requires a safe application name")
		}
		if options.UserConfigDir == "" {
			userDirectory, err := operations.userConfigDir()
			if err != nil {
				return settings{}, discoveryOperationError(err)
			}
			options.UserConfigDir = userDirectory
		}
	}

	configured := settings{Options: options, operations: operations}
	var err error
	if options.Root != "" {
		configured.rootAbs, err = operations.absolute(options.Root)
		if err != nil {
			return settings{}, discoveryOperationError(err)
		}
		configured.rootAbs = filepath.Clean(configured.rootAbs)
		configured.rootResolved, err = operations.evalSymlinks(configured.rootAbs)
		if err != nil {
			return settings{}, discoveryOperationError(err)
		}
	}
	if options.StartDir != "" {
		configured.startAbs, err = configured.absolute(options.StartDir)
		if err != nil {
			return settings{}, err
		}
		configured.startResolved, err = operations.evalSymlinks(configured.startAbs)
		if err != nil {
			return settings{}, discoveryOperationError(err)
		}
	}
	if options.StopDir != "" {
		configured.stopAbs, err = configured.absolute(options.StopDir)
		if err != nil {
			return settings{}, err
		}
		configured.stopResolved, err = operations.evalSymlinks(configured.stopAbs)
		if err != nil {
			return settings{}, discoveryOperationError(err)
		}
	}
	if options.Upward && !exactWithin(configured.stopResolved, configured.startResolved) {
		return settings{}, errors.New("upward stop directory must contain start directory")
	}
	if options.UseUserConfigDir {
		configured.UserConfigDir, err = configured.absolute(options.UserConfigDir)
		if err != nil {
			return settings{}, err
		}
	}
	return configured, nil
}

func (s settings) absolute(path string) (string, error) {
	if !filepath.IsAbs(path) && s.rootAbs != "" {
		path = filepath.Join(s.rootAbs, path)
	}
	absolute, err := s.operations.absolute(path)
	if err != nil {
		return "", discoveryOperationError(err)
	}
	absolute = filepath.Clean(absolute)
	if s.rootAbs != "" {
		if _, err := s.operations.relative(s.rootAbs, absolute); err != nil {
			return "", ErrOutsideRoot
		}
		if !exactWithin(s.rootAbs, absolute) {
			return "", ErrOutsideRoot
		}
	}
	return absolute, nil
}

func (s settings) inspect(path string, candidate candidate) (Result, bool, error) {
	info, err := s.operations.lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Result{}, false, nil
	}
	if err != nil {
		return Result{}, false, discoveryOperationError(err)
	}
	if info.IsDir() {
		return Result{}, false, nil
	}
	resolved, err := s.operations.evalSymlinks(path)
	if err != nil {
		return Result{}, false, discoveryOperationError(err)
	}
	symlink, err := s.pathContainsSymlink(path, info)
	if err != nil {
		return Result{}, false, err
	}
	if symlink && s.Symlinks == RejectSymlinks {
		return Result{}, false, ErrSymlink
	}
	if s.rootResolved != "" {
		_, err := s.operations.relative(s.rootResolved, resolved)
		if err != nil {
			return Result{}, false, discoveryOperationError(err)
		}
		if !exactWithin(s.rootResolved, resolved) {
			return Result{}, false, ErrOutsideRoot
		}
	}
	resolvedInfo, err := s.operations.stat(path)
	if err != nil {
		return Result{}, false, discoveryOperationError(err)
	}
	if !resolvedInfo.Mode().IsRegular() {
		return Result{}, false, nil
	}
	if s.Permissions == OwnerOnly && resolvedInfo.Mode().Perm()&0o077 != 0 {
		return Result{}, false, ErrPermissions
	}
	return Result{
		Path: path, ResolvedPath: resolved, Directory: candidate.directory,
		SearchPlace: candidate.searchPlace, Explicit: candidate.explicit,
		UserConfig: candidate.userConfig, Symlink: symlink,
	}, true, nil
}

func (s settings) pathContainsSymlink(
	path string,
	finalInfo os.FileInfo,
) (bool, error) {
	if isLinkLike(finalInfo) {
		return true, nil
	}
	directory := filepath.Dir(filepath.Clean(path))
	var rootInfo os.FileInfo
	for s.rootAbs == "" || !samePath(directory, s.rootAbs) {
		if s.rootAbs != "" && rootInfo == nil {
			var err error
			rootInfo, err = s.operations.lstat(s.rootAbs)
			if err != nil {
				return false, discoveryOperationError(err)
			}
		}
		info, err := s.operations.lstat(directory)
		if err != nil {
			return false, discoveryOperationError(err)
		}
		if rootInfo != nil && s.operations.sameFile(info, rootInfo) {
			break
		}
		if isLinkLike(info) {
			return true, nil
		}
		parent := filepath.Dir(directory)
		if samePath(parent, directory) {
			break
		}
		directory = parent
	}
	return false, nil
}

func visitDirectory(
	directory string,
	explicit bool,
	userConfig bool,
	places []string,
	consider func(candidate) (bool, error),
) (bool, error) {
	for _, place := range places {
		stop, err := consider(candidate{
			path: filepath.Join(directory, place), directory: directory,
			searchPlace: place, explicit: explicit, userConfig: userConfig,
		})
		if err != nil || stop {
			return stop, err
		}
	}
	return false, nil
}

func within(parent, child string) bool {
	return withinWith(parent, child, filepath.Rel)
}

func withinWith(
	parent string,
	child string,
	relativePath func(string, string) (string, error),
) bool {
	relative, err := relativePath(parent, child)
	if err != nil {
		return false
	}
	return relativeWithin(relative)
}

func relativeWithin(relative string) bool {
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func samePath(left, right string) bool {
	return filepath.Clean(left) == filepath.Clean(right)
}

func exactWithin(parent, child string) bool {
	parent = filepath.Clean(parent)
	child = filepath.Clean(child)
	if parent == child {
		return true
	}
	prefix := parent
	separator := string(filepath.Separator)
	if !strings.HasSuffix(prefix, separator) {
		prefix += separator
	}
	return strings.HasPrefix(child, prefix)
}

func discoveryOperationError(cause error) error {
	return safeerror.Redact(cause, "configuration discovery platform operation failed")
}
