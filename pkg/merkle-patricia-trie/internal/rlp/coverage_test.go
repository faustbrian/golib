package rlp

import (
	"errors"
	"testing"
)

func TestValueAccessorsRejectWrongKind(t *testing.T) {
	t.Parallel()

	if got := List(String([]byte{1})).Bytes(); got != nil {
		t.Fatalf("List.Bytes() = %x", got)
	}
	if got := String([]byte{1}).Elements(); got != nil {
		t.Fatalf("String.Elements() = %#v", got)
	}
}

func TestEncodeRejectsEveryBoundAndUnknownKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		value  Value
		limits Limits
		want   error
	}{
		{
			name:   "invalid limits",
			value:  String(nil),
			limits: Limits{},
			want:   ErrLimit,
		},
		{
			name:   "depth",
			value:  List(List()),
			limits: Limits{MaxEncodedBytes: 16, MaxDepth: 0, MaxItems: 4},
			want:   ErrLimit,
		},
		{
			name:   "items",
			value:  List(String(nil)),
			limits: Limits{MaxEncodedBytes: 16, MaxDepth: 2, MaxItems: 1},
			want:   ErrLimit,
		},
		{
			name: "list payload accumulation",
			value: List(
				String(nil), String(nil), String(nil),
			),
			limits: Limits{MaxEncodedBytes: 2, MaxDepth: 2, MaxItems: 4},
			want:   ErrLimit,
		},
		{
			name:   "unknown kind",
			value:  Value{kind: Kind(99)},
			limits: DefaultLimits(),
			want:   ErrMalformed,
		},
		{
			name:   "short payload size",
			value:  String([]byte{1, 2}),
			limits: Limits{MaxEncodedBytes: 2, MaxDepth: 1, MaxItems: 1},
			want:   ErrLimit,
		},
		{
			name:   "long payload size",
			value:  String(make([]byte, 56)),
			limits: Limits{MaxEncodedBytes: 56, MaxDepth: 1, MaxItems: 1},
			want:   ErrLimit,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Encode(test.value, test.limits); !errors.Is(err, test.want) {
				t.Fatalf("Encode() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestDecodeLongListAndInternalFailureBounds(t *testing.T) {
	t.Parallel()

	longList := append([]byte{0xf8, 56}, make([]byte, 56)...)
	for index := 2; index < len(longList); index++ {
		longList[index] = 0x80
	}
	decoded, err := Decode(longList, DefaultLimits())
	if err != nil {
		t.Fatalf("Decode(long list) error = %v", err)
	}
	if got := len(decoded.Elements()); got != 56 {
		t.Fatalf("Decode(long list) elements = %d", got)
	}

	if _, err := Decode([]byte{0x80}, Limits{}); !errors.Is(err, ErrLimit) {
		t.Fatalf("Decode(invalid limits) error = %v", err)
	}
	items := 0
	if _, _, err := decode(nil, DefaultLimits(), 0, &items); !errors.Is(err, ErrMalformed) {
		t.Fatalf("decode(empty) error = %v", err)
	}
	if _, err := Decode([]byte{0xf8, 56}, DefaultLimits()); !errors.Is(err, ErrMalformed) {
		t.Fatalf("Decode(truncated long list) error = %v", err)
	}
	overflow := []byte{0xbf, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	if _, err := Decode(overflow, DefaultLimits()); !errors.Is(err, ErrLimit) {
		t.Fatalf("Decode(length overflow) error = %v", err)
	}
}
