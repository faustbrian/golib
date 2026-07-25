package wire_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/wire"
)

func TestDetectFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload []byte
		want    wire.Format
	}{
		{name: "JSON object", payload: []byte(" \n {\"ok\":true}"), want: wire.FormatJSON},
		{name: "JSON array", payload: []byte("[1,2,3]"), want: wire.FormatJSON},
		{name: "XML", payload: []byte("\xef\xbb\xbf <?xml version=\"1.0\"?><root/>"), want: wire.FormatXML},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := wire.DetectFormat(tt.payload)
			if err != nil {
				t.Fatalf("DetectFormat() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("DetectFormat() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDetectFormatRejectsUnknownAndEmptyPayloads(t *testing.T) {
	t.Parallel()

	for _, payload := range [][]byte{nil, []byte("  \n\t"), []byte("plain text")} {
		_, err := wire.DetectFormat(payload)
		if !errors.Is(err, wire.ErrUnsupportedFormat) {
			t.Fatalf("DetectFormat(%q) error = %v, want unsupported format", payload, err)
		}

		var wireErr *wire.Error
		if !errors.As(err, &wireErr) {
			t.Fatalf("DetectFormat(%q) error type = %T, want *wire.Error", payload, err)
		}
		if wireErr.Kind != wire.ErrorKindUnsupported || wireErr.Op != "detect format" {
			t.Fatalf("DetectFormat(%q) error = %#v", payload, wireErr)
		}
	}
}

func TestErrorSupportsClassificationAndWrapping(t *testing.T) {
	t.Parallel()

	cause := errors.New("invalid character")
	err := &wire.Error{
		Kind:   wire.ErrorKindParse,
		Format: wire.FormatJSON,
		Op:     "decode",
		Err:    cause,
	}

	if got, want := err.Error(), "wire: json decode: parse failure: invalid character"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if !errors.Is(err, wire.ErrParse) {
		t.Fatal("errors.Is(err, wire.ErrParse) = false")
	}
	if !errors.Is(err, cause) {
		t.Fatal("errors.Is(err, cause) = false")
	}
	if errors.Is(err, wire.ErrValidation) {
		t.Fatal("errors.Is(err, wire.ErrValidation) = true")
	}
}

func TestErrorFormatsMissingOptionalFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  *wire.Error
		want string
	}{
		{name: "kind only", err: &wire.Error{Kind: wire.ErrorKindValidation}, want: "wire: validation failure"},
		{name: "operation and cause", err: &wire.Error{Op: "read body", Err: errors.New("too large")}, want: "wire: read body: too large"},
		{name: "zero value", err: &wire.Error{}, want: "wire: failure"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.err.Error(); got != tt.want {
				t.Fatalf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestErrorKindsMatchTheirSentinels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind     wire.ErrorKind
		sentinel error
	}{
		{kind: wire.ErrorKindParse, sentinel: wire.ErrParse},
		{kind: wire.ErrorKindValidation, sentinel: wire.ErrValidation},
		{kind: wire.ErrorKindUnsupported, sentinel: wire.ErrUnsupportedFormat},
		{kind: wire.ErrorKindEnvelope, sentinel: wire.ErrEnvelope},
		{kind: wire.ErrorKindFault, sentinel: wire.ErrSOAPFault},
		{kind: wire.ErrorKindWrite, sentinel: wire.ErrWrite},
		{kind: wire.ErrorKindSizeLimit, sentinel: wire.ErrSizeLimit},
		{kind: wire.ErrorKindTarget, sentinel: wire.ErrTarget},
		{kind: wire.ErrorKindEncode, sentinel: wire.ErrEncode},
	}

	for _, tt := range tests {
		err := &wire.Error{Kind: tt.kind}
		if !errors.Is(err, tt.sentinel) {
			t.Errorf("errors.Is(%q, %q) = false", tt.kind, tt.sentinel)
		}
	}
}
