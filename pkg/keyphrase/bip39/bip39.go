// Package bip39 implements BIP-39 mnemonic encoding, validation, language
// detection, and seed derivation. It does not implement wallets, addresses,
// private-key custody, BIP-32, or BIP-44.
package bip39

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"strings"

	keyphrase "github.com/faustbrian/golib/pkg/keyphrase"
	"github.com/faustbrian/golib/pkg/keyphrase/wordlist"
	"golang.org/x/text/unicode/norm"
)

const (
	maxMnemonicBytes   = 64 << 10
	maxPassphraseBytes = 1 << 20
	pbkdf2Rounds       = 2048
	seedSize           = 64
)

// ErrorCode identifies a mnemonic operation failure.
type ErrorCode string

const (
	// CodeInvalidEntropy reports an unsupported entropy size.
	CodeInvalidEntropy ErrorCode = "invalid_entropy"
	// CodeUnsupportedLanguage reports a language outside the official set.
	CodeUnsupportedLanguage ErrorCode = "unsupported_language"
	// CodeListIntegrity reports a pinned-list validation failure.
	CodeListIntegrity ErrorCode = "list_integrity"
	// CodeInvalidLength reports an unsupported mnemonic word count.
	CodeInvalidLength ErrorCode = "invalid_length"
	// CodeUnknownWord reports vocabulary absent from the selected list.
	CodeUnknownWord ErrorCode = "unknown_word"
	// CodeAmbiguousLanguage reports vocabulary shared by multiple lists.
	CodeAmbiguousLanguage ErrorCode = "ambiguous_language"
	// CodeChecksum reports mnemonic checksum failure.
	CodeChecksum ErrorCode = "checksum"
	// CodeRandomness reports entropy-source failure.
	CodeRandomness ErrorCode = "randomness"
	// CodeCanceled reports context cancellation.
	CodeCanceled ErrorCode = "canceled"
	// CodeInvalidGenerator reports a nil selector.
	CodeInvalidGenerator ErrorCode = "invalid_generator"
	// CodeOversized reports input above resource limits.
	CodeOversized ErrorCode = "oversized"
)

// Error never contains mnemonic, entropy, passphrase, or seed material.
type Error struct {
	Code       ErrorCode
	Cause      error
	Candidates []Language
}

func (e *Error) Error() string {
	return fmt.Sprintf("bip39: operation failed (%s)", e.Code)
}

func (e *Error) Unwrap() error {
	return e.Cause
}

// Format prevents wrapped diagnostics from appearing in debug output.
func (e *Error) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, e.Error())
}

// MarshalText omits wrapped diagnostics from encoded output.
func (e *Error) MarshalText() ([]byte, error) {
	return []byte(e.Error()), nil
}

// Mnemonic is an immutable validated BIP-39 mnemonic.
type Mnemonic struct {
	language Language
	words    []string
	entropy  []byte
}

// FromEntropy creates a mnemonic from one of the official entropy sizes.
func FromEntropy(entropy []byte, language Language) (Mnemonic, error) {
	if !validEntropyBytes(len(entropy)) {
		return Mnemonic{}, &Error{Code: CodeInvalidEntropy}
	}
	list, err := List(language)
	if err != nil {
		return Mnemonic{}, err
	}
	return fromEntropy(entropy, language, list)
}

func fromEntropy(entropy []byte, language Language, list interface {
	Word(int) (string, bool)
}) (Mnemonic, error) {
	hash := sha256.Sum256(entropy)
	entropyBits := len(entropy) * 8
	checksumBits := entropyBits / 32
	wordCount := (entropyBits + checksumBits) / 11
	words := make([]string, wordCount)
	for wordPosition := range wordCount {
		index := 0
		for bit := range 11 {
			offset := wordPosition*11 + bit
			index <<= 1
			if offset < entropyBits {
				index |= bitAt(entropy, offset)
			} else {
				index |= bitAt(hash[:], offset-entropyBits)
			}
		}
		word, exists := list.Word(index)
		if !exists {
			return Mnemonic{}, &Error{Code: CodeListIntegrity}
		}
		words[wordPosition] = word
	}

	return Mnemonic{language: language, words: words, entropy: append([]byte(nil), entropy...)}, nil
}

// Generate creates fresh entropy and its corresponding mnemonic.
func Generate(ctx context.Context, entropyBits int, language Language, selector *keyphrase.Selector) (Mnemonic, error) {
	if selector == nil {
		return Mnemonic{}, &Error{Code: CodeInvalidGenerator}
	}
	if !validEntropyBytes(entropyBits/8) || entropyBits%8 != 0 {
		return Mnemonic{}, &Error{Code: CodeInvalidEntropy}
	}
	entropy := make([]byte, entropyBits/8)
	if err := selector.Fill(ctx, entropy); err != nil {
		clear(entropy)
		return Mnemonic{}, &Error{Code: CodeRandomness, Cause: err}
	}
	mnemonic, err := FromEntropy(entropy, language)
	clear(entropy)
	return mnemonic, err
}

// Parse detects the official language and validates the checksum. Vocabulary
// shared by multiple lists is reported as ambiguity instead of guessed.
func Parse(phrase string) (Mnemonic, error) {
	words, err := normalizedWords(phrase)
	if err != nil {
		return Mnemonic{}, err
	}

	return parseDetected(words, List)
}

func parseDetected(words []string, provider func(Language) (*wordlist.List, error)) (Mnemonic, error) {
	candidates, err := detectLanguage(words, provider)
	if err != nil {
		return Mnemonic{}, err
	}
	if len(candidates) == 0 {
		return Mnemonic{}, &Error{Code: CodeUnknownWord}
	}
	if len(candidates) > 1 {
		return Mnemonic{}, &Error{Code: CodeAmbiguousLanguage, Candidates: append([]Language(nil), candidates...)}
	}

	return parseWords(words, candidates[0])
}

func detectLanguage(words []string, provider func(Language) (*wordlist.List, error)) ([]Language, error) {
	candidates := make([]Language, 0, 2)
	for _, language := range officialLanguages {
		list, listErr := provider(language)
		if listErr != nil {
			return nil, listErr
		}
		matches := true
		for _, word := range words {
			if _, exists := list.Index(word); !exists {
				matches = false
				break
			}
		}
		if matches {
			candidates = append(candidates, language)
		}
	}
	return candidates, nil
}

// ParseLanguage validates phrase against an explicitly selected language.
func ParseLanguage(phrase string, language Language) (Mnemonic, error) {
	words, err := normalizedWords(phrase)
	if err != nil {
		return Mnemonic{}, err
	}
	return parseWords(words, language)
}

// String returns the canonical mnemonic sentence. Japanese uses ideographic
// spaces as shown in the official vectors; seed derivation still applies NFKD.
func (m Mnemonic) String() string {
	separator := " "
	if m.language == Japanese {
		separator = "\u3000"
	}
	return strings.Join(m.words, separator)
}

// Format prevents mnemonic and entropy disclosure through formatting verbs.
// Call String explicitly only at a reviewed integration boundary.
func (Mnemonic) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, "bip39.Mnemonic{redacted}")
}

// LogValue prevents mnemonic disclosure through log/slog.
func (Mnemonic) LogValue() slog.Value {
	return slog.StringValue("bip39.Mnemonic{redacted}")
}

// MarshalText prevents mnemonic disclosure through standard encoders.
func (Mnemonic) MarshalText() ([]byte, error) {
	return []byte("bip39.Mnemonic{redacted}"), nil
}

// Words returns a copy of the mnemonic words.
func (m Mnemonic) Words() []string {
	return append([]string(nil), m.words...)
}

// Language returns the detected or selected official language.
func (m Mnemonic) Language() Language {
	return m.language
}

// Entropy returns a caller-owned copy of the validated source entropy.
func (m Mnemonic) Entropy() keyphrase.Secret {
	return append(keyphrase.Secret(nil), m.entropy...)
}

// Seed derives the 64-byte BIP-39 seed using NFKD and PBKDF2-HMAC-SHA512 with
// 2,048 iterations. The returned buffer is caller-owned.
func Seed(ctx context.Context, mnemonic Mnemonic, passphrase string) (keyphrase.Secret, error) {
	if !validWordCount(len(mnemonic.words)) || !validEntropyBytes(len(mnemonic.entropy)) {
		return nil, &Error{Code: CodeInvalidLength}
	}
	if len(passphrase) > maxPassphraseBytes {
		return nil, &Error{Code: CodeOversized}
	}
	if err := ctx.Err(); err != nil {
		return nil, &Error{Code: CodeCanceled, Cause: err}
	}
	passwordBytes := []byte(norm.NFKD.String(mnemonic.String()))
	salt := []byte(norm.NFKD.String("mnemonic" + passphrase))
	defer clear(passwordBytes)
	defer clear(salt)

	mac := hmac.New(sha512.New, passwordBytes)
	_, _ = mac.Write(salt)
	var block [4]byte
	binary.BigEndian.PutUint32(block[:], 1)
	_, _ = mac.Write(block[:])
	u := mac.Sum(nil)
	result := append(keyphrase.Secret(nil), u...)
	//nolint:revive // Keep mutation tests bounded without changing PBKDF2.
	for iteration := 1; iteration < pbkdf2Rounds; iteration += 1 {
		if iteration%64 == 0 {
			if err := ctx.Err(); err != nil {
				clear(u)
				clear(result)
				return nil, &Error{Code: CodeCanceled, Cause: err}
			}
		}
		mac.Reset()
		_, _ = mac.Write(u)
		next := mac.Sum(nil)
		clear(u)
		u = next
		for index := range result {
			result[index] ^= u[index]
		}
	}
	clear(u)

	return result[:seedSize], nil
}

func parseWords(words []string, language Language) (Mnemonic, error) {
	list, err := List(language)
	if err != nil {
		return Mnemonic{}, err
	}
	indices := make([]int, len(words))
	for index, word := range words {
		wordIndex, exists := list.Index(word)
		if !exists {
			return Mnemonic{}, &Error{Code: CodeUnknownWord}
		}
		indices[index] = wordIndex
	}

	totalBits := len(words) * 11
	entropyBits := totalBits * 32 / 33
	entropy := make([]byte, entropyBits/8)
	for offset := range entropyBits {
		index := indices[offset/11]
		bit := (index >> (10 - offset%11)) & 1
		if bit == 1 {
			entropy[offset/8] |= byte(1 << (7 - offset%8))
		}
	}
	hash := sha256.Sum256(entropy)
	for offset := entropyBits; offset < totalBits; offset++ {
		index := indices[offset/11]
		mnemonicBit := (index >> (10 - offset%11)) & 1
		checksumBit := bitAt(hash[:], offset-entropyBits)
		if mnemonicBit != checksumBit {
			clear(entropy)
			return Mnemonic{}, &Error{Code: CodeChecksum}
		}
	}

	return Mnemonic{language: language, words: append([]string(nil), words...), entropy: entropy}, nil
}

func normalizedWords(phrase string) ([]string, error) {
	if len(phrase) == 0 || len(phrase) > maxMnemonicBytes {
		return nil, &Error{Code: CodeOversized}
	}
	words := strings.Fields(norm.NFKD.String(phrase))
	if !validWordCount(len(words)) {
		return nil, &Error{Code: CodeInvalidLength}
	}
	return words, nil
}

func validEntropyBytes(length int) bool {
	return length == 16 || length == 20 || length == 24 || length == 28 || length == 32
}

func validWordCount(length int) bool {
	return length == 12 || length == 15 || length == 18 || length == 21 || length == 24
}

func bitAt(encoded []byte, offset int) int {
	return int(encoded[offset/8] >> (7 - offset%8) & 1)
}
