package xmlwire

import (
	"encoding/xml"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/wire"
)

func TestExactBoundaryPredicates(t *testing.T) {
	t.Parallel()

	if exceedsLimit(4, 4) || !exceedsLimit(5, 4) {
		t.Fatal("exceedsLimit() did not preserve the exact boundary")
	}
	for _, value := range []byte{0x80, 0x9f} {
		if !isWindows1252Control(value) {
			t.Fatalf("isWindows1252Control(0x%02x) = false", value)
		}
	}
	for _, value := range []byte{0x7f, 0xa0} {
		if isWindows1252Control(value) {
			t.Fatalf("isWindows1252Control(0x%02x) = true", value)
		}
	}
}

func TestClassifyDecodeErrorRecognizesIndependentParseErrors(t *testing.T) {
	t.Parallel()

	for _, err := range []error{
		&xml.SyntaxError{Msg: "malformed", Line: 1},
		charsetError{message: "unsupported"},
	} {
		if classified := classifyDecodeError("decode", err); !errors.Is(classified, wire.ErrParse) {
			t.Fatalf("classifyDecodeError(%T) = %v", err, classified)
		}
	}
}
