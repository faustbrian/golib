package parameter

import (
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

func TestValidOptionsAcceptsFormAtBothDefinedLocations(t *testing.T) {
	t.Parallel()

	version, err := specversion.Parse("3.2.0")
	if err != nil {
		t.Fatal(err)
	}
	for _, location := range []Location{Query, CookieLocation} {
		if !validOptions(jsonvalue.StringKind, Options{
			Version: version, Location: location, Style: Form,
		}) {
			t.Fatalf("form location %q rejected", location)
		}
	}
	if validOptions(jsonvalue.StringKind, Options{
		Version: version, Location: Path, Style: Form,
	}) {
		t.Fatal("form path accepted")
	}
}

func TestUnreservedCharacterEndpoints(t *testing.T) {
	t.Parallel()

	for _, character := range []byte("AZaz09-._~") {
		if !isUnreserved(character) {
			t.Fatalf("unreserved character %q rejected", character)
		}
	}
	if isUnreserved('@') {
		t.Fatal("reserved character accepted")
	}
}
