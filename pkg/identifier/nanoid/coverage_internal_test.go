package nanoid

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	identifier "github.com/faustbrian/golib/pkg/identifier"
)

type repeatingReader byte

func (reader repeatingReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = byte(reader)
	}

	return len(buffer), nil
}

func TestRemainingNanoIDBoundaries(t *testing.T) {
	if _, err := ParseWithConfig("x", Config{}); !errors.Is(err, identifier.ErrInvalid) {
		t.Fatalf("invalid parse config error = %v", err)
	}
	if _, err := Prepare(Config{}); !errors.Is(err, identifier.ErrInvalid) {
		t.Fatalf("invalid prepare config error = %v", err)
	}
	left, _ := Parse("_____________________")
	right, _ := Parse("---------------------")
	if left.Compare(right) <= 0 {
		t.Fatal("NanoID lexical comparison failed")
	}

	var zero ID
	if _, err := zero.MarshalText(); err == nil {
		t.Fatal("zero text must fail")
	}
	if data, err := json.Marshal(zero); err != nil || string(data) != "null" {
		t.Fatalf("zero JSON = %s, %v", data, err)
	}
	if err := json.Unmarshal([]byte("null"), &left); err != nil || !left.IsZero() {
		t.Fatalf("JSON null = %s, %v", left, err)
	}

	defaultGenerator, err := NewGenerator(DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, generateErr := defaultGenerator.New(); generateErr != nil {
		t.Fatal(generateErr)
	}

	alphabet := "!\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqr"
	config := Config{Alphabet: alphabet, Size: 20}
	if validateErr := config.Validate(); validateErr != nil {
		t.Fatal(validateErr)
	}
	rejecting, err := NewGenerator(config, repeatingReader(0xff))
	if err != nil {
		t.Fatal(err)
	}
	if _, generateErr := rejecting.New(); !errors.Is(generateErr, identifier.ErrEntropy) {
		t.Fatalf("rejection-limit error = %v", generateErr)
	}

	mixed := append(bytes.Repeat([]byte{0xff}, 8), bytes.Repeat([]byte{0}, 64)...)
	accepted, err := NewGenerator(config, bytes.NewReader(mixed))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := accepted.New(); err != nil {
		t.Fatal(err)
	}
}

func TestGeneratorMaskUsesSmallestCoveringBitRange(t *testing.T) {
	alphabet := "!\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~"
	for _, test := range []struct {
		length int
		mask   byte
	}{
		{length: 2, mask: 1},
		{length: 3, mask: 3},
		{length: 64, mask: 63},
		{length: 65, mask: 127},
		{length: 94, mask: 127},
	} {
		generator, err := NewGenerator(
			Config{Alphabet: alphabet[:test.length], Size: 120},
			repeatingReader(0),
		)
		if err != nil {
			t.Fatalf("alphabet length %d: %v", test.length, err)
		}
		if generator.mask != test.mask {
			t.Fatalf(
				"alphabet length %d mask = %d, want %d",
				test.length,
				generator.mask,
				test.mask,
			)
		}
	}
}
