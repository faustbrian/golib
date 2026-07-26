package openrpc_test

import (
	"errors"
	"testing"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
)

func TestParseVersionAcceptsSupportedPatchLine(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"1.4.0", "1.4.1", "1.4.999"} {
		version, err := openrpc.ParseVersion(input)
		if err != nil {
			t.Errorf("ParseVersion(%q): %v", input, err)
			continue
		}
		if version.String() != input {
			t.Errorf("ParseVersion(%q).String() = %q", input, version.String())
		}
		if version.FeatureSet() != "1.4" {
			t.Errorf("ParseVersion(%q).FeatureSet() = %q", input, version.FeatureSet())
		}
	}
}

func TestParseVersionRejectsUnsupportedOrMalformedVersions(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		"",
		"1.3.2",
		"1.5.0",
		"2.0.0",
		"v1.4.1",
		"1.4",
		"1.4.01",
		"1.4.1-alpha",
		"1.4.1+build",
	} {
		_, err := openrpc.ParseVersion(input)
		if !errors.Is(err, openrpc.ErrUnsupportedVersion) {
			t.Errorf("ParseVersion(%q) error = %v, want ErrUnsupportedVersion", input, err)
		}
	}
}

func TestSupportedVersionsReturnsAnOwnedSnapshot(t *testing.T) {
	t.Parallel()

	versions := openrpc.SupportedVersions()
	if len(versions) != 1 || versions[0] != "1.4.x" {
		t.Fatalf("SupportedVersions() = %#v", versions)
	}
	versions[0] = "changed"
	if openrpc.SupportedVersions()[0] != "1.4.x" {
		t.Fatal("SupportedVersions exposed mutable package state")
	}
}
