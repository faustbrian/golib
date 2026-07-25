package bip39_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/keyphrase/bip39"
)

func TestPinnedOfficialListsLoad(t *testing.T) {
	t.Parallel()

	if len(bip39.Languages()) != 10 {
		t.Fatalf("Languages() length = %d, want 10", len(bip39.Languages()))
	}
	for _, language := range bip39.Languages() {
		list, err := bip39.List(language)
		if err != nil {
			var listError interface{ Unwrap() error }
			if errors.As(err, &listError) {
				t.Fatalf("List(%q) error = %v, cause = %v", language, err, listError.Unwrap())
			}
			t.Fatalf("List(%q) error = %v", language, err)
		}
		if list.Len() != 2048 {
			t.Fatalf("List(%q) length = %d, want 2048", language, list.Len())
		}
		metadata := list.Metadata()
		if metadata.Source == "" || metadata.Version == "" || metadata.License == "" || metadata.SHA256 == "" || metadata.SourceSHA256 == "" {
			t.Fatalf("List(%q) metadata incomplete: %#v", language, metadata)
		}
	}
}

func TestOfficialVectorsAcrossEveryOfficialLanguage(t *testing.T) {
	t.Parallel()

	encoded, err := os.ReadFile("testdata/vectors.json")
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var vectors map[string][][]string
	if err := json.Unmarshal(encoded, &vectors); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	for _, language := range bip39.Languages() {
		t.Run(string(language), func(t *testing.T) {
			t.Parallel()

			fixtures := vectors[string(language)]
			if len(fixtures) == 0 {
				t.Fatal("no official vectors")
			}
			for vectorIndex, vector := range fixtures {
				entropy, decodeErr := hex.DecodeString(vector[0])
				if decodeErr != nil {
					t.Fatalf("vector %d entropy: %v", vectorIndex, decodeErr)
				}
				mnemonic, mnemonicErr := bip39.FromEntropy(entropy, language)
				if mnemonicErr != nil {
					t.Fatalf("vector %d FromEntropy() error = %v", vectorIndex, mnemonicErr)
				}
				if mnemonic.String() != vector[1] {
					t.Fatalf("vector %d mnemonic mismatch", vectorIndex)
				}

				parsed, parseErr := bip39.ParseLanguage(vector[1], language)
				if parseErr != nil {
					t.Fatalf("vector %d ParseLanguage() error = %v", vectorIndex, parseErr)
				}
				if parsed.String() != vector[1] {
					t.Fatalf("vector %d parse round trip mismatch", vectorIndex)
				}

				seed, seedErr := bip39.Seed(context.Background(), parsed, "TREZOR")
				if seedErr != nil {
					t.Fatalf("vector %d Seed() error = %v", vectorIndex, seedErr)
				}
				if hex.EncodeToString(seed) != vector[2] {
					t.Fatalf("vector %d seed mismatch", vectorIndex)
				}
				clear(seed)
			}
		})
	}
}

func TestParseDetectsLanguageAmbiguityAndChecksumErrors(t *testing.T) {
	t.Parallel()

	_, err := bip39.Parse(strings.Repeat("一 ", 11) + "一")
	var mnemonicError *bip39.Error
	if !errors.As(err, &mnemonicError) || mnemonicError.Code != bip39.CodeAmbiguousLanguage || len(mnemonicError.Candidates) < 2 {
		t.Fatalf("Parse() error = %#v, want language ambiguity", err)
	}

	invalid := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon"
	_, err = bip39.ParseLanguage(invalid, bip39.English)
	if !errors.As(err, &mnemonicError) || mnemonicError.Code != bip39.CodeChecksum {
		t.Fatalf("ParseLanguage() error = %v, want checksum", err)
	}
}

func TestEveryEntropySizeRoundTrips(t *testing.T) {
	t.Parallel()

	for _, bits := range []int{128, 160, 192, 224, 256} {
		entropy := make([]byte, bits/8)
		for index := range entropy {
			entropy[index] = byte(index)
		}
		mnemonic, err := bip39.FromEntropy(entropy, bip39.English)
		if err != nil {
			t.Fatalf("FromEntropy(%d) error = %v", bits, err)
		}
		if got := mnemonic.Entropy(); !strings.EqualFold(hex.EncodeToString(got), hex.EncodeToString(entropy)) {
			t.Fatalf("Entropy(%d) mismatch", bits)
		}
	}
}

func TestMnemonicRoundTripAndChecksumProperties(t *testing.T) {
	t.Parallel()

	list, err := bip39.List(bip39.English)
	if err != nil {
		t.Fatalf("List(English) error = %v", err)
	}
	for _, bits := range []int{128, 160, 192, 224, 256} {
		for sample := range 8 {
			digest := sha256.Sum256(fmt.Appendf(nil, "%d/%d", bits, sample))
			entropy := digest[:bits/8]
			mnemonic, mnemonicErr := bip39.FromEntropy(entropy, bip39.English)
			if mnemonicErr != nil {
				t.Fatalf("FromEntropy(%d, %d) error = %v", bits, sample, mnemonicErr)
			}
			parsed, parseErr := bip39.ParseLanguage(mnemonic.String(), bip39.English)
			if parseErr != nil {
				t.Fatalf("ParseLanguage(%d, %d) error = %v", bits, sample, parseErr)
			}
			parsedEntropy := parsed.Entropy()
			if !bytes.Equal(parsedEntropy, entropy) {
				t.Fatalf("round trip (%d, %d) changed entropy", bits, sample)
			}
			clear(parsedEntropy)

			words := mnemonic.Words()
			last := len(words) - 1
			wordIndex, exists := list.Index(words[last])
			if !exists {
				t.Fatalf("word %q missing from English list", words[last])
			}
			words[last], exists = list.Word(wordIndex ^ 1)
			if !exists {
				t.Fatalf("checksum mutation index %d missing", wordIndex^1)
			}
			_, checksumErr := bip39.ParseLanguage(strings.Join(words, " "), bip39.English)
			var mnemonicError *bip39.Error
			if !errors.As(checksumErr, &mnemonicError) || mnemonicError.Code != bip39.CodeChecksum {
				t.Fatalf("checksum mutation (%d, %d) error = %v", bits, sample, checksumErr)
			}
		}
	}
}
