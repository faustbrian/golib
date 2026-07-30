package profile

import (
	"errors"
	"testing"
)

func TestExperimentalProfileIsExactAndRejectsOtherValues(t *testing.T) {
	t.Parallel()

	value := ExperimentalBandersnatchIPA256V0()
	if value.ID() != BandersnatchIPA256V0 ||
		value.Name() != bandersnatchIPA256V0Name ||
		value.Version() != 0 ||
		!value.Experimental() ||
		value.BranchingWidth() != 256 ||
		value.KeySize() != 32 ||
		value.StemSize() != 31 ||
		value.ValueSize() != 32 ||
		value.EncodingVersion() != 1 {
		t.Fatalf("unexpected profile: %#v", value)
	}
	if err := value.Validate(); err != nil {
		t.Fatalf("validate exact profile: %v", err)
	}
	if err := (Profile{}).Validate(); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("zero profile error = %v", err)
	}
}
