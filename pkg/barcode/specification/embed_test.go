package specification_test

import (
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/barcode/specification"
)

func TestGS1SyntaxDictionaryIsEmbedded(t *testing.T) {
	dictionary := specification.GS1SyntaxDictionary()
	if len(dictionary) < 10_000 || !strings.Contains(dictionary, "GS1 Barcode Syntax Dictionary") {
		t.Fatalf("embedded dictionary is missing or truncated: %d bytes", len(dictionary))
	}
}
