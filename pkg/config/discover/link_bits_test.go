package discover

import (
	"io/fs"
	"testing"
)

func TestLinkFlagHelpersRequireTheRequestedBit(t *testing.T) {
	t.Parallel()

	if !hasModeFlag(fs.ModeSymlink|fs.ModeDir, fs.ModeSymlink) {
		t.Fatal("hasModeFlag() = false for present flag")
	}
	if hasModeFlag(fs.ModeDir, fs.ModeSymlink) {
		t.Fatal("hasModeFlag() = true for absent flag")
	}
	if !hasUint32Flag(0b110, 0b010) {
		t.Fatal("hasUint32Flag() = false for present flag")
	}
	if hasUint32Flag(0b100, 0b010) {
		t.Fatal("hasUint32Flag() = true for absent flag")
	}
}
