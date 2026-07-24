package password_test

import (
	"context"
	"errors"
	"testing"

	keyphrase "github.com/faustbrian/golib/pkg/keyphrase"
	"github.com/faustbrian/golib/pkg/keyphrase/password"
)

func TestBytePolicySupportsArbitraryByteAlphabets(t *testing.T) {
	t.Parallel()

	selector, err := keyphrase.NewSelector(repeatingSource{value: 0})
	if err != nil {
		t.Fatalf("NewSelector() error = %v", err)
	}
	generator, err := password.NewGenerator(selector)
	if err != nil {
		t.Fatalf("NewGenerator() error = %v", err)
	}
	policy := password.BytePolicy{
		Length:   3,
		Alphabet: []byte{0x00, 0x7f, 0xff},
		Required: []password.ByteClass{{Name: "binary", Bytes: []byte{0xff}}},
	}

	distribution, err := password.AnalyzeBytes(policy)
	if err != nil {
		t.Fatalf("AnalyzeBytes() error = %v", err)
	}
	if distribution.Outcomes.String() != "19" {
		t.Fatalf("outcomes = %s, want 19", distribution.Outcomes)
	}

	destination := []byte{9, 9, 9}
	n, err := generator.GenerateBytesInto(context.Background(), destination, policy)
	if err != nil {
		t.Fatalf("GenerateBytesInto() error = %v", err)
	}
	if n != 3 || destination[0] != 0 || destination[1] != 0 || destination[2] != 0xff {
		t.Fatalf("GenerateBytesInto() = %v, %d, want [0 0 255], 3", destination, n)
	}
}

func TestBytePolicyRejectsDuplicatesAndPreservesSmallBuffers(t *testing.T) {
	t.Parallel()

	_, err := password.AnalyzeBytes(password.BytePolicy{Length: 2, Alphabet: []byte{1, 1}})
	var policyError *password.Error
	if !errors.As(err, &policyError) || policyError.Code != password.CodeDuplicateSymbol {
		t.Fatalf("AnalyzeBytes() error = %v, want duplicate symbol", err)
	}

	destination := []byte{7}
	n, err := password.DefaultGenerator().GenerateBytesInto(
		context.Background(),
		destination,
		password.BytePolicy{Length: 2, Alphabet: []byte{1, 2}},
	)
	if n != 0 || destination[0] != 7 {
		t.Fatalf("small destination changed: %v, %d", destination, n)
	}
	if !errors.As(err, &policyError) || policyError.Code != password.CodeBufferTooSmall {
		t.Fatalf("GenerateBytesInto() error = %v, want buffer too small", err)
	}
}
