package bip39

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	keyphrase "github.com/faustbrian/golib/pkg/keyphrase"
	"github.com/faustbrian/golib/pkg/keyphrase/wordlist"
	"golang.org/x/text/unicode/norm"
)

type fillSource struct {
	value byte
	err   error
}

func (s fillSource) ReadContext(_ context.Context, destination []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	for index := range destination {
		destination[index] = s.value
	}
	return len(destination), nil
}

type cancelAfterChecks struct {
	checks int
}

type emptyMnemonicList struct{}

func (emptyMnemonicList) Word(int) (string, bool) { return "", false }

func (c *cancelAfterChecks) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *cancelAfterChecks) Done() <-chan struct{}       { return nil }
func (c *cancelAfterChecks) Value(any) any               { return nil }
func (c *cancelAfterChecks) Err() error {
	c.checks++
	if c.checks >= 2 {
		return context.Canceled
	}
	return nil
}

type countingContext struct {
	checks int
}

func (c *countingContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *countingContext) Done() <-chan struct{}       { return nil }
func (c *countingContext) Value(any) any               { return nil }
func (c *countingContext) Err() error {
	c.checks++
	return nil
}

func TestGenerateUsesInjectedRandomnessWithoutPartialResults(t *testing.T) {
	t.Parallel()

	selector, _ := keyphrase.NewSelector(fillSource{value: 0})
	mnemonic, err := Generate(context.Background(), 128, English, selector)
	if err != nil || mnemonic.String() != "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about" {
		t.Fatalf("Generate() = %q, %v", mnemonic.String(), err)
	}
	if mnemonic.Language() != English || len(mnemonic.Words()) != 12 {
		t.Fatal("generated mnemonic accessors mismatch")
	}
	if formatted := fmt.Sprintf("%#v", mnemonic); strings.Contains(formatted, "abandon") || !strings.Contains(formatted, "redacted") {
		t.Fatalf("mnemonic formatting disclosed content: %q", formatted)
	}

	if _, err := Generate(context.Background(), 127, English, selector); errorCode(err) != CodeInvalidEntropy {
		t.Fatalf("Generate(127) code = %q", errorCode(err))
	}
	if _, err := Generate(context.Background(), 128, English, nil); errorCode(err) != CodeInvalidGenerator {
		t.Fatalf("Generate(nil) code = %q", errorCode(err))
	}
	sourceFailure := errors.New("device failure")
	failing, _ := keyphrase.NewSelector(fillSource{err: sourceFailure})
	if mnemonic, err := Generate(context.Background(), 128, English, failing); errorCode(err) != CodeRandomness || len(mnemonic.words) != 0 {
		t.Fatalf("Generate(failing) = %#v, %v", mnemonic, err)
	}
}

func TestMnemonicInputFailuresAreTypedAndSecretSafe(t *testing.T) {
	t.Parallel()

	if _, err := FromEntropy(make([]byte, 15), English); errorCode(err) != CodeInvalidEntropy {
		t.Fatalf("FromEntropy(short) code = %q", errorCode(err))
	}
	if _, err := FromEntropy(make([]byte, 16), "unknown"); errorCode(err) != CodeUnsupportedLanguage {
		t.Fatalf("FromEntropy(language) code = %q", errorCode(err))
	}
	if _, err := Parse("not enough words"); errorCode(err) != CodeInvalidLength {
		t.Fatalf("Parse(short) code = %q", errorCode(err))
	}
	if _, err := Parse(strings.Repeat("x", maxMnemonicBytes+1)); errorCode(err) != CodeOversized {
		t.Fatalf("Parse(oversized) code = %q", errorCode(err))
	}
	if _, err := Parse(strings.Repeat("x", maxMnemonicBytes)); errorCode(err) != CodeInvalidLength {
		t.Fatalf("Parse(maximum encoded bytes) code = %q", errorCode(err))
	}
	unknown := strings.Repeat("notaword ", 11) + "notaword"
	if _, err := Parse(unknown); errorCode(err) != CodeUnknownWord {
		t.Fatalf("Parse(unknown) code = %q", errorCode(err))
	}
	if _, err := ParseLanguage(unknown, English); errorCode(err) != CodeUnknownWord {
		t.Fatalf("ParseLanguage(unknown) code = %q", errorCode(err))
	}
	if _, err := ParseLanguage("not enough words", English); errorCode(err) != CodeInvalidLength {
		t.Fatalf("ParseLanguage(short) code = %q", errorCode(err))
	}
	if _, err := fromEntropy(make([]byte, 16), English, emptyMnemonicList{}); errorCode(err) != CodeListIntegrity {
		t.Fatalf("fromEntropy(short list) code = %q", errorCode(err))
	}
	if _, err := ParseLanguage(strings.Repeat("abandon ", 11)+"about", "unknown"); errorCode(err) != CodeUnsupportedLanguage {
		t.Fatalf("ParseLanguage(language) code = %q", errorCode(err))
	}
	validPhrase := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	if mnemonic, err := Parse(validPhrase); err != nil || mnemonic.Language() != English {
		t.Fatalf("Parse(valid English) = %#v, %v", mnemonic, err)
	}
	whitespacePhrase := strings.Join(strings.Fields(validPhrase), "\t\n")
	if mnemonic, err := Parse(whitespacePhrase); err != nil || mnemonic.String() != validPhrase {
		t.Fatalf("Parse(noncanonical whitespace) failed: %v", err)
	}
	spanish, err := List(Spanish)
	if err != nil {
		t.Fatalf("List(Spanish) error = %v", err)
	}
	spanishWord, _ := spanish.Word(0)
	mixedPhrase := strings.Repeat("abandon ", 11) + spanishWord
	if _, err := Parse(mixedPhrase); errorCode(err) != CodeUnknownWord {
		t.Fatalf("Parse(mixed language) code = %q", errorCode(err))
	}
	japanese, err := FromEntropy(make([]byte, 16), Japanese)
	if err != nil {
		t.Fatalf("FromEntropy(Japanese) error = %v", err)
	}
	composedJapanese := norm.NFC.String(japanese.String())
	if parsed, err := ParseLanguage(composedJapanese, Japanese); err != nil || parsed.String() != japanese.String() {
		t.Fatalf("ParseLanguage(NFC Japanese) failed: %v", err)
	}

	mnemonicError := &Error{Code: CodeChecksum, Cause: errors.New("cause")}
	if mnemonicError.Error() != "bip39: operation failed (checksum)" || mnemonicError.Unwrap() == nil {
		t.Fatal("Error contract mismatch")
	}
}

func TestListLoadingFailurePaths(t *testing.T) {
	t.Parallel()

	if _, err := loadOne(fstest.MapFS{}, English, checksums[English]); errorCode(err) != CodeListIntegrity {
		t.Fatalf("loadOne(missing) code = %q", errorCode(err))
	}
	malformed := fstest.MapFS{"data/english.txt": &fstest.MapFile{Data: []byte("only-one-word\n")}}
	if _, err := loadOne(malformed, English, strings.Repeat("0", 64)); errorCode(err) != CodeListIntegrity {
		t.Fatalf("loadOne(malformed) code = %q", errorCode(err))
	}
	if _, err := loadAll(fstest.MapFS{}); errorCode(err) != CodeListIntegrity {
		t.Fatalf("loadAll(missing) code = %q", errorCode(err))
	}
	loadFailure := &Error{Code: CodeListIntegrity}
	if _, err := resolveList(English, nil, loadFailure); !errors.Is(err, loadFailure) {
		t.Fatalf("resolveList(error) = %v", err)
	}
	providerFailure := errors.New("provider failure")
	if _, err := detectLanguage([]string{"word"}, func(Language) (*wordlist.List, error) {
		return nil, providerFailure
	}); !errors.Is(err, providerFailure) {
		t.Fatalf("detectLanguage(error) = %v", err)
	}
	if _, err := parseDetected([]string{"word"}, func(Language) (*wordlist.List, error) {
		return nil, providerFailure
	}); !errors.Is(err, providerFailure) {
		t.Fatalf("parseDetected(error) = %v", err)
	}
}

func TestSeedCancellationReturnsNoPartialSeed(t *testing.T) {
	t.Parallel()

	mnemonic, err := FromEntropy(make([]byte, 16), English)
	if err != nil {
		t.Fatalf("FromEntropy() error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if seed, err := Seed(canceled, mnemonic, "secret"); !errors.Is(err, context.Canceled) || seed != nil {
		t.Fatalf("Seed(pre-canceled) = %v, %v", seed, err)
	}
	ctx := &cancelAfterChecks{}
	if seed, err := Seed(ctx, mnemonic, "secret"); !errors.Is(err, context.Canceled) || seed != nil {
		t.Fatalf("Seed(mid-derivation) = %v, %v", seed, err)
	}
	if seed, err := Seed(context.Background(), Mnemonic{}, "secret"); errorCode(err) != CodeInvalidLength || seed != nil {
		t.Fatalf("Seed(zero mnemonic) = %v, %v", seed, err)
	}
	if seed, err := Seed(context.Background(), mnemonic, strings.Repeat("x", maxPassphraseBytes+1)); errorCode(err) != CodeOversized || seed != nil {
		t.Fatalf("Seed(oversized passphrase) = %v, %v", seed, err)
	}
	maximumSeed, err := Seed(context.Background(), mnemonic, strings.Repeat("x", maxPassphraseBytes))
	if err != nil || len(maximumSeed) != seedSize {
		t.Fatalf("Seed(maximum passphrase) length = %d, error = %v", len(maximumSeed), err)
	}
	clear(maximumSeed)
	counting := &countingContext{}
	seed, err := Seed(counting, mnemonic, "count checks")
	if err != nil {
		t.Fatalf("Seed(counting context) error = %v", err)
	}
	clear(seed)
	if counting.checks != 32 {
		t.Fatalf("context checks = %d, want 32", counting.checks)
	}
	composedSeed, err := Seed(context.Background(), mnemonic, "é")
	if err != nil {
		t.Fatalf("Seed(composed passphrase) error = %v", err)
	}
	decomposedSeed, err := Seed(context.Background(), mnemonic, "e\u0301")
	if err != nil {
		t.Fatalf("Seed(decomposed passphrase) error = %v", err)
	}
	if string(composedSeed) != string(decomposedSeed) {
		t.Fatal("NFKD-equivalent passphrases produced different seeds")
	}
	clear(composedSeed)
	clear(decomposedSeed)
}

func errorCode(err error) ErrorCode {
	var mnemonicError *Error
	if errors.As(err, &mnemonicError) {
		return mnemonicError.Code
	}
	return ""
}
