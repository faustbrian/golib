package phone_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/international/country"
	"github.com/faustbrian/golib/pkg/international/internationaltest"
	"github.com/faustbrian/golib/pkg/international/phone"
	"github.com/nyaruka/phonenumbers"
)

func TestDifferentialAgainstPinnedLibphonenumber(t *testing.T) {
	t.Parallel()
	// These are frozen public example-number ranges, not personal data.
	for _, vector := range internationaltest.PhoneVectors() {
		t.Run(vector.Region, func(t *testing.T) {
			region, err := country.Parse(vector.Region)
			if err != nil {
				t.Fatal(err)
			}
			got, err := phone.Parse(vector.Input, phone.ParseOptions{RegionHint: region})
			if err != nil {
				t.Fatalf("phone.Parse() error = %v", err)
			}
			upstream, err := phonenumbers.Parse(vector.Input, vector.Region)
			if err != nil {
				t.Fatalf("phonenumbers.Parse() error = %v", err)
			}
			national, _ := got.Format(phone.FormatNational)
			international, _ := got.Format(phone.FormatInternational)
			if got.E164() != phonenumbers.Format(upstream, phonenumbers.E164) ||
				got.Possible() != phonenumbers.IsPossibleNumber(upstream) ||
				got.Valid() != phonenumbers.IsValidNumber(upstream) ||
				national != phonenumbers.Format(upstream, phonenumbers.NATIONAL) ||
				international != phonenumbers.Format(upstream, phonenumbers.INTERNATIONAL) {
				t.Fatalf("differential mismatch for public %s vector", vector.Region)
			}
		})
	}
}
