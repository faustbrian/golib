package datatype_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/xsd/datatype"
)

func TestBuiltInDerivationMetadata(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		local  string
		base   string
		method string
		ok     bool
	}{
		{local: "token", base: "normalizedString", method: "restriction", ok: true},
		{local: "IDREFS", base: "anySimpleType", method: "list", ok: true},
		{local: "unknown"},
	} {
		base, method, ok := datatype.BuiltInDerivation(test.local)
		if base != test.base || method != test.method || ok != test.ok {
			t.Fatalf("BuiltInDerivation(%q) = %q, %q, %t", test.local, base, method, ok)
		}
		base, ok = datatype.BuiltInBase(test.local)
		if base != test.base || ok != test.ok {
			t.Fatalf("BuiltInBase(%q) = %q, %t", test.local, base, ok)
		}
	}
}
