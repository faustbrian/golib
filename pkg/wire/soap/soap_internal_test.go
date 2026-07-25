package soap

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/wire"
)

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
