package localizedconfig_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/international/locale"
)

func mustLocale(t testing.TB, raw string) locale.Tag {
	t.Helper()
	tag, err := locale.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := tag.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}
