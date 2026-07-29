package verkletree_test

import (
	"errors"
	"testing"

	verkletree "github.com/faustbrian/golib/pkg/verkle-tree"
)

func TestExperimentalBandersnatchIPA256V0(t *testing.T) {
	t.Parallel()

	profile := verkletree.ExperimentalBandersnatchIPA256V0()

	if err := profile.Validate(); err != nil {
		t.Fatalf("validate profile: %v", err)
	}
	if got, want := profile.ID(), verkletree.ProfileBandersnatchIPA256V0; got != want {
		t.Fatalf("profile ID = %d, want %d", got, want)
	}
	if got, want := profile.Name(), "verkletree-bandersnatch-ipa-256-v0"; got != want {
		t.Fatalf("profile name = %q, want %q", got, want)
	}
	if got, want := profile.Version(), uint16(0); got != want {
		t.Fatalf("profile version = %d, want %d", got, want)
	}
	if !profile.Experimental() {
		t.Fatal("profile must remain experimental")
	}
	if got, want := profile.BranchingWidth(), uint16(256); got != want {
		t.Fatalf("branching width = %d, want %d", got, want)
	}
	if got, want := profile.KeySize(), uint16(32); got != want {
		t.Fatalf("key size = %d, want %d", got, want)
	}
	if got, want := profile.StemSize(), uint16(31); got != want {
		t.Fatalf("stem size = %d, want %d", got, want)
	}
	if got, want := profile.ValueSize(), uint16(32); got != want {
		t.Fatalf("value size = %d, want %d", got, want)
	}
	if got, want := profile.EncodingVersion(), uint16(1); got != want {
		t.Fatalf("encoding version = %d, want %d", got, want)
	}
}

func TestZeroProfileIsUnsupported(t *testing.T) {
	t.Parallel()

	var profile verkletree.Profile

	if err := profile.Validate(); !errors.Is(err, verkletree.ErrUnsupportedProfile) {
		t.Fatalf("validate zero profile error = %v, want ErrUnsupportedProfile", err)
	}
}
