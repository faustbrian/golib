package discover

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func FuzzDiscoveryBoundary(f *testing.F) {
	f.Add("config.yaml", "config.yaml", uint8(0), uint8(0))
	f.Add("../outside.yaml", "../config.yaml", uint8(1), uint8(1))
	f.Add("symlink.yaml", "config/../../secret", uint8(2), uint8(2))
	f.Add("config\x00.yaml", "..", uint8(255), uint8(255))

	f.Fuzz(func(t *testing.T, candidatePath, searchPlace string, policy, flags uint8) {
		if len(candidatePath)+len(searchPlace) > 1024 {
			t.Skip()
		}
		operations := fuzzDiscoveryOperations(flags)
		ctx := context.Background()
		if flags&0x20 != 0 {
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			ctx = canceled
		}
		results, err := search(ctx, Options{
			Root:             "/sandbox",
			Explicit:         []string{candidatePath},
			Directories:      []string{"config"},
			SearchPlaces:     []string{searchPlace},
			Mode:             Mode(policy % 3),
			Symlinks:         SymlinkPolicy((policy / 3) % 3),
			Permissions:      PermissionPolicy((policy / 9) % 3),
			MaxCandidates:    4,
			MaxResults:       4,
			MaxUpwardDepth:   4,
			UseUserConfigDir: flags&0x40 != 0,
			UserConfigDir:    "user",
			Application:      searchPlace,
		}, operations)
		if err != nil {
			return
		}
		if len(results) > 4 {
			t.Fatalf("search returned %d results above configured bound", len(results))
		}
		for _, result := range results {
			if !within("/sandbox", result.Path) || !within("/sandbox", result.ResolvedPath) {
				t.Fatalf("search returned path outside root: %#v", result)
			}
		}
	})
}

func fuzzDiscoveryOperations(flags uint8) fileOperations {
	absolute := func(path string) (string, error) {
		if filepath.IsAbs(path) {
			return filepath.Clean(path), nil
		}
		return filepath.Clean(filepath.Join("/working", path)), nil
	}
	return fileOperations{
		absolute: absolute,
		evalSymlinks: func(path string) (string, error) {
			if strings.Contains(path, "symlink") {
				return filepath.Join("/outside", filepath.Base(path)), nil
			}
			return filepath.Clean(path), nil
		},
		lstat: func(path string) (os.FileInfo, error) {
			if strings.Contains(path, "missing") {
				return nil, fs.ErrNotExist
			}
			return fuzzDiscoveryFileInfo{name: filepath.Base(path), mode: 0o600}, nil
		},
		stat: func(path string) (os.FileInfo, error) {
			mode := fs.FileMode(0o600)
			if flags&1 != 0 {
				mode = 0o644
			}
			return fuzzDiscoveryFileInfo{name: filepath.Base(path), mode: mode}, nil
		},
		sameFile: func(left, right os.FileInfo) bool {
			return left.Name() == right.Name()
		},
		relative:      filepath.Rel,
		userConfigDir: func() (string, error) { return "/sandbox/user", nil },
	}
}

type fuzzDiscoveryFileInfo struct {
	name string
	mode fs.FileMode
}

func (i fuzzDiscoveryFileInfo) Name() string       { return i.name }
func (i fuzzDiscoveryFileInfo) Size() int64        { return 1 }
func (i fuzzDiscoveryFileInfo) Mode() fs.FileMode  { return i.mode }
func (i fuzzDiscoveryFileInfo) ModTime() time.Time { return time.Time{} }
func (i fuzzDiscoveryFileInfo) IsDir() bool        { return i.mode.IsDir() }
func (i fuzzDiscoveryFileInfo) Sys() any           { return nil }
