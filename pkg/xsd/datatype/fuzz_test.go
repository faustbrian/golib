package datatype_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/xsd/datatype"
)

func FuzzParseDecimal(f *testing.F) {
	for _, seed := range []string{"0", "-0.5", "+001.2300", "1e3", "NaN"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, lexical string) {
		value, err := datatype.ParseDecimal(lexical)
		if err != nil {
			return
		}
		canonical, err := datatype.ParseDecimal(value.String())
		if err != nil || canonical.Compare(value) != 0 {
			t.Fatalf("canonical round trip = %v, %v", canonical, err)
		}
	})
}

func FuzzBuiltInLexical(f *testing.F) {
	f.Add("language", "en-US")
	f.Add("dateTime", "2000-02-29T12:00:00Z")
	f.Add("hexBinary", "0aFE")
	f.Fuzz(func(t *testing.T, name string, lexical string) {
		_ = datatype.ValidateBuiltInLexical(name, lexical)
	})
}
