package discover

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestPathContainsSymlinkRejectsWindowsReparseComponent(t *testing.T) {
	t.Parallel()

	root := `C:\root`
	reparse := filepath.Join(root, "junction")
	path := filepath.Join(reparse, "config.yaml")
	operations := defaultFileOperations
	operations.lstat = func(candidate string) (os.FileInfo, error) {
		attributes := uint32(0)
		mode := fs.FileMode(0)
		if candidate == root {
			attributes = syscall.FILE_ATTRIBUTE_DIRECTORY
			mode = fs.ModeDir
		}
		if candidate == reparse {
			attributes = syscall.FILE_ATTRIBUTE_REPARSE_POINT |
				syscall.FILE_ATTRIBUTE_DIRECTORY
			mode = fs.ModeIrregular
		}
		return windowsFileInfo{attributes: attributes, mode: mode}, nil
	}
	operations.sameFile = func(os.FileInfo, os.FileInfo) bool { return false }
	configured := settings{rootAbs: root, operations: operations}
	link, err := configured.pathContainsSymlink(path, windowsFileInfo{})
	if err != nil {
		t.Fatalf("pathContainsSymlink() error = %v", err)
	}
	if !link {
		t.Fatal("pathContainsSymlink() = false, want true for reparse component")
	}
}

type windowsFileInfo struct {
	attributes uint32
	mode       fs.FileMode
}

func (windowsFileInfo) Name() string           { return "entry" }
func (windowsFileInfo) Size() int64            { return 0 }
func (info windowsFileInfo) Mode() fs.FileMode { return info.mode }
func (windowsFileInfo) ModTime() time.Time     { return time.Time{} }
func (info windowsFileInfo) IsDir() bool       { return info.mode.IsDir() }
func (info windowsFileInfo) Sys() any {
	return &syscall.Win32FileAttributeData{FileAttributes: info.attributes}
}
