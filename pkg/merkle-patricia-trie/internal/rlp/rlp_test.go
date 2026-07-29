package rlp

import (
	"errors"
	"slices"
	"testing"
)

func TestEncodeCanonicalVectors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value Value
		want  []byte
	}{
		{name: "empty string", value: String(nil), want: []byte{0x80}},
		{name: "single byte below 0x80", value: String([]byte{0x00}), want: []byte{0x00}},
		{name: "single byte at 0x80", value: String([]byte{0x80}), want: []byte{0x81, 0x80}},
		{name: "dog", value: String([]byte("dog")), want: []byte{0x83, 'd', 'o', 'g'}},
		{name: "empty list", value: List(), want: []byte{0xc0}},
		{
			name:  "cat dog list",
			value: List(String([]byte("cat")), String([]byte("dog"))),
			want:  []byte{0xc8, 0x83, 'c', 'a', 't', 0x83, 'd', 'o', 'g'},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := Encode(test.value, DefaultLimits())
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			if !slices.Equal(got, test.want) {
				t.Fatalf("Encode() = %x, want %x", got, test.want)
			}
		})
	}
}

func TestEncodeShortAndLongLengthBoundary(t *testing.T) {
	t.Parallel()

	for _, length := range []int{55, 56, 255, 256} {
		payload := make([]byte, length)
		for index := range payload {
			payload[index] = byte(index)
		}
		encoded, err := Encode(String(payload), DefaultLimits())
		if err != nil {
			t.Fatalf("Encode(%d bytes) error = %v", length, err)
		}

		var prefix []byte
		switch length {
		case 55:
			prefix = []byte{0xb7}
		case 56:
			prefix = []byte{0xb8, 0x38}
		case 255:
			prefix = []byte{0xb8, 0xff}
		case 256:
			prefix = []byte{0xb9, 0x01, 0x00}
		}
		if !slices.Equal(encoded[:len(prefix)], prefix) {
			t.Fatalf("Encode(%d bytes) prefix = %x, want %x", length, encoded[:len(prefix)], prefix)
		}

		decoded, err := Decode(encoded, DefaultLimits())
		if err != nil {
			t.Fatalf("Decode(%d bytes) error = %v", length, err)
		}
		if decoded.Kind() != KindString || !slices.Equal(decoded.Bytes(), payload) {
			t.Fatalf("Decode(%d bytes) did not preserve payload", length)
		}
	}
}

func TestDecodeNestedListAndOwnsBytes(t *testing.T) {
	t.Parallel()

	encoded := []byte{0xc4, 0x01, 0xc2, 0x81, 0x80}
	decoded, err := Decode(encoded, DefaultLimits())
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	encoded[1] = 0x09

	elements := decoded.Elements()
	if len(elements) != 2 || !slices.Equal(elements[0].Bytes(), []byte{0x01}) {
		t.Fatalf("Elements() = %#v", elements)
	}
	nested := elements[1].Elements()
	if len(nested) != 1 || !slices.Equal(nested[0].Bytes(), []byte{0x80}) {
		t.Fatalf("nested Elements() = %#v", nested)
	}

	first := elements[0].Bytes()
	first[0] = 0xff
	if !slices.Equal(decoded.Elements()[0].Bytes(), []byte{0x01}) {
		t.Fatal("decoded string aliases returned bytes")
	}
}

func TestDecodeRejectsMalformedAndNonCanonicalInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data []byte
		want error
	}{
		{name: "empty input", data: nil, want: ErrMalformed},
		{name: "trailing value", data: []byte{0x80, 0x80}, want: ErrMalformed},
		{name: "short string truncated", data: []byte{0x82, 0x01}, want: ErrMalformed},
		{name: "single byte overlong", data: []byte{0x81, 0x01}, want: ErrNonCanonical},
		{name: "long string used below 56", data: []byte{0xb8, 0x01, 0x80}, want: ErrNonCanonical},
		{name: "long string leading zero length", data: []byte{0xb9, 0x00, 0x38}, want: ErrNonCanonical},
		{name: "long string truncated length", data: []byte{0xb9, 0x01}, want: ErrMalformed},
		{name: "long string truncated payload", data: []byte{0xb8, 0x38, 0x01}, want: ErrMalformed},
		{name: "short list truncated child", data: []byte{0xc1, 0x81}, want: ErrMalformed},
		{name: "long list used below 56", data: []byte{0xf8, 0x01, 0x80}, want: ErrNonCanonical},
		{name: "long list leading zero length", data: []byte{0xf9, 0x00, 0x38}, want: ErrNonCanonical},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := Decode(test.data, DefaultLimits())
			if !errors.Is(err, test.want) {
				t.Fatalf("Decode(%x) error = %v, want %v", test.data, err, test.want)
			}
		})
	}
}

func TestLimitsRejectWorkBeforeDecoding(t *testing.T) {
	t.Parallel()

	_, err := Decode([]byte{0x82, 0x01, 0x02}, Limits{
		MaxEncodedBytes: 2,
		MaxDepth:        2,
		MaxItems:        2,
	})
	if !errors.Is(err, ErrLimit) {
		t.Fatalf("Decode() error = %v, want ErrLimit", err)
	}

	_, err = Decode([]byte{0xc2, 0xc1, 0xc0}, Limits{
		MaxEncodedBytes: 16,
		MaxDepth:        1,
		MaxItems:        16,
	})
	if !errors.Is(err, ErrLimit) {
		t.Fatalf("Decode() depth error = %v, want ErrLimit", err)
	}

	_, err = Decode([]byte{0xc2, 0x01, 0x02}, Limits{
		MaxEncodedBytes: 16,
		MaxDepth:        4,
		MaxItems:        2,
	})
	if !errors.Is(err, ErrLimit) {
		t.Fatalf("Decode() item error = %v, want ErrLimit", err)
	}
}

func TestValueConstructorsAndAccessorsDoNotAlias(t *testing.T) {
	t.Parallel()

	source := []byte{1, 2, 3}
	child := String(source)
	parent := List(child)
	source[0] = 9

	children := parent.Elements()
	children[0] = String([]byte{9})
	if !slices.Equal(parent.Elements()[0].Bytes(), []byte{1, 2, 3}) {
		t.Fatal("Value aliases constructor or accessor state")
	}
}
