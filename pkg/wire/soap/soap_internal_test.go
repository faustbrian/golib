package soap

import (
	"bytes"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/wire"
)

func TestExceedsLimitHonorsExactBoundary(t *testing.T) {
	t.Parallel()

	if exceedsLimit(4, 4) {
		t.Fatal("exceedsLimit() rejected exact limit")
	}
	if !exceedsLimit(5, 4) {
		t.Fatal("exceedsLimit() accepted value above limit")
	}
}

func TestMakeFaultRequiresCodeAndReasonIndependently(t *testing.T) {
	t.Parallel()

	tests := []rawFault{
		{Reason: rawReason{Texts: []rawReasonText{{Text: "reason"}}}},
		{Code: rawFaultCode{Value: "env:Sender"}},
	}
	for _, raw := range tests {
		if _, err := makeFault(Version12, raw, nil); !errors.Is(err, wire.ErrEnvelope) {
			t.Fatalf("makeFault() error = %v", err)
		}
	}
}

func TestWriteOptionalElementDistinguishesEmptyAndPresentValues(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	if err := writeOptionalElement(&output, "role", ""); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("empty output = %q", output.String())
	}
	if err := writeOptionalElement(&output, "role", "a&b"); err != nil {
		t.Fatal(err)
	}
	if output.String() != "<role>a&amp;b</role>" {
		t.Fatalf("present output = %q", output.String())
	}
}

func TestDecodeBodyClassifiesCorruptCachedEnvelope(t *testing.T) {
	t.Parallel()

	namespace := ` xmlns:env="http://schemas.xmlsoap.org/soap/envelope/"`
	tests := []struct {
		name string
		raw  string
		kind error
	}{
		{name: "outer token", raw: `<env:Envelope` + namespace + `><`, kind: wire.ErrParse},
		{name: "skipped header", raw: `<env:Envelope` + namespace + `><env:Header><`, kind: wire.ErrParse},
		{name: "body token", raw: `<env:Envelope` + namespace + `><env:Body><`, kind: wire.ErrParse},
		{name: "second child", raw: `<env:Envelope` + namespace + `><env:Body><one/><two><`, kind: wire.ErrParse},
		{name: "body text", raw: `<env:Envelope` + namespace + `><env:Body>text</env:Body></env:Envelope>`, kind: wire.ErrEnvelope},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			envelope := &Envelope{Version: Version11, raw: []byte(tt.raw)}
			var target struct{}
			err := envelope.DecodeBody(&target)
			if !errors.Is(err, tt.kind) {
				t.Fatalf("DecodeBody() error = %v, want %v", err, tt.kind)
			}
		})
	}
}
