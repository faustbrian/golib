package rlp

import (
	"errors"
	"slices"
	"testing"
)

func TestEncodeLimitsAreInclusiveAtEveryFramingBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		value  Value
		limits Limits
		want   []byte
	}{
		{
			name:   "exact depth",
			value:  List(String(nil)),
			limits: Limits{MaxEncodedBytes: 2, MaxDepth: 1, MaxItems: 2},
			want:   []byte{0xc1, 0x80},
		},
		{
			name: "exact accumulated list payload",
			value: List(
				String(nil),
				String(nil),
			),
			limits: Limits{MaxEncodedBytes: 3, MaxDepth: 1, MaxItems: 3},
			want:   []byte{0xc2, 0x80, 0x80},
		},
		{
			name:   "exact short payload",
			value:  String([]byte{1, 2}),
			limits: Limits{MaxEncodedBytes: 3, MaxDepth: 0, MaxItems: 1},
			want:   []byte{0x82, 1, 2},
		},
		{
			name:   "exact long payload",
			value:  String(make([]byte, 56)),
			limits: Limits{MaxEncodedBytes: 58, MaxDepth: 0, MaxItems: 1},
			want:   append([]byte{0xb8, 56}, make([]byte, 56)...),
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			encoded, err := Encode(test.value, test.limits)
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			if !slices.Equal(encoded, test.want) {
				t.Fatalf("Encode() = %x, want %x", encoded, test.want)
			}
		})
	}

	if _, err := Encode(
		List(String(nil), String(nil)),
		Limits{MaxEncodedBytes: 2, MaxDepth: 1, MaxItems: 3},
	); !errors.Is(err, ErrLimit) {
		t.Fatalf("Encode(over accumulated payload) error = %v", err)
	}
	if _, err := Encode(
		String(make([]byte, 56)),
		Limits{MaxEncodedBytes: 57, MaxDepth: 0, MaxItems: 1},
	); !errors.Is(err, ErrLimit) {
		t.Fatalf("Encode(over long payload) error = %v", err)
	}
}

func TestListPayloadReservationBoundsEveryAppend(t *testing.T) {
	t.Parallel()

	payload, err := appendListPayload([]byte{1}, []byte{2}, 3)
	if err != nil || !slices.Equal(payload, []byte{1, 2}) {
		t.Fatalf("appendListPayload(exact) = (%x, %v)", payload, err)
	}
	if _, err := appendListPayload(
		[]byte{1}, []byte{2, 3}, 3,
	); !errors.Is(err, ErrLimit) {
		t.Fatalf("appendListPayload(over limit) error = %v", err)
	}
}

func TestDecodePrefixAndDepthBoundariesAreInclusive(t *testing.T) {
	t.Parallel()

	single, err := Decode(
		[]byte{0x7f},
		Limits{MaxEncodedBytes: 1, MaxDepth: 0, MaxItems: 1},
	)
	if err != nil || !slices.Equal(single.Bytes(), []byte{0x7f}) {
		t.Fatalf("Decode(0x7f) = (%x, %v)", single.Bytes(), err)
	}

	nested, err := Decode(
		[]byte{0xc1, 0xc0},
		Limits{MaxEncodedBytes: 2, MaxDepth: 1, MaxItems: 2},
	)
	if err != nil || len(nested.Elements()) != 1 {
		t.Fatalf("Decode(exact depth) = (%#v, %v)", nested, err)
	}

	shortList := append([]byte{0xf7}, make([]byte, 55)...)
	decoded, err := Decode(
		shortList,
		Limits{MaxEncodedBytes: 56, MaxDepth: 1, MaxItems: 56},
	)
	if err != nil || len(decoded.Elements()) != 55 {
		t.Fatalf("Decode(0xf7 list) elements = %d, error = %v", len(decoded.Elements()), err)
	}
}

func TestLongPayloadLengthAcceptsTheLargestInt(t *testing.T) {
	t.Parallel()

	encoded := []byte{
		0xbf,
		0x7f, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	}
	length, offset, err := longPayloadLength(encoded, 8)
	if err != nil {
		t.Fatalf("longPayloadLength(maximum int) error = %v", err)
	}
	if length != int(^uint(0)>>1) || offset != len(encoded) {
		t.Fatalf("longPayloadLength(maximum int) = (%d, %d)", length, offset)
	}
}

func TestEveryRLPLimitIsValidatedIndependently(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		invalidate func(*Limits)
	}{
		{"encoded bytes", func(limits *Limits) { limits.MaxEncodedBytes = 0 }},
		{"depth", func(limits *Limits) { limits.MaxDepth = -1 }},
		{"items", func(limits *Limits) { limits.MaxItems = 0 }},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			limits := Limits{MaxEncodedBytes: 1, MaxDepth: 0, MaxItems: 1}
			test.invalidate(&limits)
			if err := validateLimits(limits); !errors.Is(err, ErrLimit) {
				t.Fatalf("validateLimits() error = %v", err)
			}
		})
	}
	if err := validateLimits(
		Limits{MaxEncodedBytes: 1, MaxDepth: 0, MaxItems: 1},
	); err != nil {
		t.Fatalf("validateLimits(exact minima) error = %v", err)
	}
}
