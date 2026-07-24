package xsd_test

import (
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
)

func TestNormalizeIdentityXPathWhitespace(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		input string
		want  string
	}{
		{input: "  . // p:item / @ id | child  ", want: ".//p:item/@id|child"},
		{input: "a b", want: "a b"},
		{input: "p :name", want: "p :name"},
		{input: "\t.\n", want: "."},
	} {
		if got := xsd.NormalizeIdentityXPath(test.input); got != test.want {
			t.Fatalf("NormalizeIdentityXPath(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}
