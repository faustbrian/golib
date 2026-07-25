package phone

import (
	"errors"
	"strings"
	"testing"

	international "github.com/faustbrian/golib/pkg/international"
	"github.com/nyaruka/phonenumbers"
)

func TestNumberTypeMappingCoversPinnedLibphonenumberEnum(t *testing.T) {
	t.Parallel()

	tests := []struct {
		upstream phonenumbers.PhoneNumberType
		public   NumberType
	}{
		{phonenumbers.FIXED_LINE, TypeFixedLine},
		{phonenumbers.MOBILE, TypeMobile},
		{phonenumbers.FIXED_LINE_OR_MOBILE, TypeFixedLineOrMobile},
		{phonenumbers.TOLL_FREE, TypeTollFree},
		{phonenumbers.PREMIUM_RATE, TypePremiumRate},
		{phonenumbers.SHARED_COST, TypeSharedCost},
		{phonenumbers.VOIP, TypeVOIP},
		{phonenumbers.PERSONAL_NUMBER, TypePersonal},
		{phonenumbers.PAGER, TypePager},
		{phonenumbers.UAN, TypeUAN},
		{phonenumbers.VOICEMAIL, TypeVoicemail},
		{phonenumbers.UNKNOWN, TypeUnknown},
		{phonenumbers.PhoneNumberType(255), TypeUnknown},
	}

	for _, test := range tests {
		if got := mapNumberType(test.upstream); got != test.public {
			t.Errorf("mapNumberType(%d) = %d, want %d", test.upstream, got, test.public)
		}
	}
}

func TestSnapshotRejectsMalformedDependencyExtension(t *testing.T) {
	t.Parallel()

	parsed, err := phonenumbers.Parse("+1 650 253 0000", "ZZ")
	if err != nil {
		t.Fatalf("phonenumbers.Parse() error = %v", err)
	}
	extension := strings.Repeat("1", MaxExtensionBytes+1)
	parsed.Extension = &extension
	if _, err := snapshotParsed(parsed); !errors.Is(err, international.ErrInvalid) {
		t.Fatalf("snapshotParsed() error = %v, want ErrInvalid", err)
	}
}

func TestSnapshotRejectsMalformedDependencyCallingCode(t *testing.T) {
	t.Parallel()
	parsed, err := phonenumbers.Parse("+1 650 253 0000", "ZZ")
	if err != nil {
		t.Fatal(err)
	}
	invalid := int32(0)
	parsed.CountryCode = &invalid
	if _, err := snapshotParsed(parsed); !errors.Is(err, international.ErrInvalid) {
		t.Fatalf("snapshotParsed() error = %v", err)
	}
}
