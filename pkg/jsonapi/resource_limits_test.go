package jsonapi

import (
	"errors"
	"testing"
)

func TestDecodeLimitsRejectExcessiveDocumentsBeforeSemanticDecoding(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		payload string
		limits  DecodeLimits
		path    string
	}{
		"bytes": {
			payload: `{"data":null}`,
			limits:  DecodeLimits{MaxDocumentBytes: 4},
		},
		"depth": {
			payload: `{"meta":{"nested":{"value":true}}}`,
			limits:  DecodeLimits{MaxNestingDepth: 2},
			path:    "/meta/nested",
		},
		"object members": {
			payload: `{"data":null,"meta":{}}`,
			limits:  DecodeLimits{MaxObjectMembers: 1},
		},
		"array items": {
			payload: `{"errors":[{"status":"400"},{"status":"401"}]}`,
			limits:  DecodeLimits{MaxArrayItems: 1},
			path:    "/errors",
		},
		"total values": {
			payload: `{"meta":{"one":1,"two":2}}`,
			limits:  DecodeLimits{MaxTotalValues: 3},
			path:    "/meta/two",
		},
		"total composite values": {
			payload: `{"meta":{}}`,
			limits:  DecodeLimits{MaxTotalValues: 1},
			path:    "/meta",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := UnmarshalWithLimits(
				[]byte(test.payload),
				ValidationOptions{},
				test.limits,
			)
			var decodeError *DecodeError
			if !errors.As(err, &decodeError) || decodeError.Code != "limit" ||
				decodeError.Path != test.path {
				t.Fatalf("unexpected limit error: %T %#v", err, decodeError)
			}
		})
	}
}

func TestDecodeLimitsApplyToAtomicAndConfiguredCodecs(t *testing.T) {
	t.Parallel()

	limits := DecodeLimits{MaxArrayItems: 1}
	_, err := UnmarshalAtomicWithLimits(
		[]byte(`{"atomic:operations":[{"op":"remove","href":"/1"},{"op":"remove","href":"/2"}]}`),
		AtomicValidationOptions{},
		limits,
	)
	var decodeError *DecodeError
	if !errors.As(err, &decodeError) || decodeError.Path != "/atomic:operations" ||
		decodeError.Code != "limit" {
		t.Fatalf("unexpected Atomic limit error: %T %#v", err, decodeError)
	}

	codec, err := NewCodec(CodecOptions{Limits: DecodeLimits{MaxDocumentBytes: 4}})
	if err != nil {
		t.Fatalf("construct limited codec: %v", err)
	}
	_, err = codec.Unmarshal([]byte(`{"data":null}`))
	if !errors.As(err, &decodeError) || decodeError.Code != "limit" {
		t.Fatalf("configured codec ignored limits: %T %#v", err, decodeError)
	}
}

func TestDecodeLimitConfiguration(t *testing.T) {
	t.Parallel()

	defaults := DefaultDecodeLimits()
	if defaults.MaxDocumentBytes < 1 || defaults.MaxNestingDepth < 1 ||
		defaults.MaxObjectMembers < 1 || defaults.MaxArrayItems < 1 ||
		defaults.MaxTotalValues < 1 {
		t.Fatalf("unsafe default limits: %#v", defaults)
	}
	if _, err := NewCodec(CodecOptions{
		Limits: DecodeLimits{MaxNestingDepth: -1},
	}); err == nil {
		t.Fatal("expected invalid codec limits error")
	}
	if _, err := UnmarshalWithLimits(
		[]byte(`{"data":null}`),
		ValidationOptions{},
		DecodeLimits{MaxArrayItems: -1},
	); err == nil {
		t.Fatal("expected invalid standalone limits error")
	}
	if _, err := UnmarshalAtomicWithLimits(
		[]byte(`{"meta":{}}`),
		AtomicValidationOptions{},
		DecodeLimits{MaxObjectMembers: -1},
	); err == nil {
		t.Fatal("expected invalid Atomic limits error")
	}
}
