package bip39_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/keyphrase/bip39"
	reference "github.com/tyler-smith/go-bip39"
	"golang.org/x/text/unicode/norm"
)

func TestEnglishParsingAndChecksumInteroperability(t *testing.T) {
	t.Parallel()

	for _, bits := range []int{128, 160, 192, 224, 256} {
		for sample := range 4 {
			digest := sha256.Sum256(fmt.Appendf(nil, "%d/%d", bits, sample))
			entropy := digest[:bits/8]
			wantPhrase, err := reference.NewMnemonic(entropy)
			if err != nil {
				t.Fatalf("reference.NewMnemonic(%d, %d) error = %v", bits, sample, err)
			}
			gotMnemonic, err := bip39.FromEntropy(entropy, bip39.English)
			if err != nil {
				t.Fatalf("bip39.FromEntropy(%d, %d) error = %v", bits, sample, err)
			}
			if gotMnemonic.String() != wantPhrase {
				t.Fatalf("mnemonic (%d, %d) differs from independent implementation", bits, sample)
			}

			parsed, err := bip39.ParseLanguage(wantPhrase, bip39.English)
			if err != nil {
				t.Fatalf("bip39.ParseLanguage(%d, %d) error = %v", bits, sample, err)
			}
			gotEntropy := parsed.Entropy()
			wantEntropy, referenceErr := reference.EntropyFromMnemonic(wantPhrase)
			if referenceErr != nil || !bytes.Equal(gotEntropy, wantEntropy) {
				t.Fatalf("parsed entropy (%d, %d) differs from independent implementation", bits, sample)
			}
			clear(gotEntropy)
			clear(wantEntropy)
		}
	}
}

func TestChecksumRejectionMatchesIndependentImplementation(t *testing.T) {
	t.Parallel()

	mnemonic, err := bip39.FromEntropy(make([]byte, 16), bip39.English)
	if err != nil {
		t.Fatalf("bip39.FromEntropy() error = %v", err)
	}
	words := strings.Fields(mnemonic.String())
	list, err := bip39.List(bip39.English)
	if err != nil {
		t.Fatalf("bip39.List() error = %v", err)
	}

	for index := 1; index < list.Len(); index++ {
		words[len(words)-1], _ = list.Word(index)
		candidate := strings.Join(words, " ")
		_, gotErr := bip39.ParseLanguage(candidate, bip39.English)
		_, wantErr := reference.EntropyFromMnemonic(candidate)
		if gotErr != nil && wantErr != nil {
			return
		}
	}
	t.Fatal("could not construct a checksum rejected by both implementations")
}

func TestSeedNormalizationAndParametersMatchIndependentImplementation(t *testing.T) {
	t.Parallel()

	mnemonic, err := bip39.FromEntropy(make([]byte, 16), bip39.Japanese)
	if err != nil {
		t.Fatalf("bip39.FromEntropy() error = %v", err)
	}
	normalizationInput := "㍍ガバヴァぱばぐゞちぢ十人十色"
	gotSeed, err := bip39.Seed(context.Background(), mnemonic, normalizationInput)
	if err != nil {
		t.Fatalf("bip39.Seed() error = %v", err)
	}
	defer clear(gotSeed)

	// The reference implementation owns PBKDF2. Supplying its inputs in the
	// specification-required form makes any normalization or parameter drift
	// in this package observable as a seed mismatch.
	wantSeed := reference.NewSeed(
		norm.NFKD.String(mnemonic.String()),
		norm.NFKD.String(normalizationInput),
	)
	defer clear(wantSeed)
	if !bytes.Equal(gotSeed, wantSeed) {
		t.Fatal("normalized seed differs from independent implementation")
	}
}
