// Package wordlist validates and exposes immutable cryptographic word lists.
package wordlist

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	maxWords    = 1 << 16
	maxWordSize = 1 << 10
)

// ErrorCode identifies a word-list validation failure.
type ErrorCode string

const (
	// CodeEmpty reports a list without entries.
	CodeEmpty ErrorCode = "empty"
	// CodeDuplicate reports an exactly repeated entry.
	CodeDuplicate ErrorCode = "duplicate"
	// CodeNormalizationCollision reports entries with equal NFKD forms.
	CodeNormalizationCollision ErrorCode = "normalization_collision"
	// CodeNormalizationRequired reports an entry that is not already NFKD.
	CodeNormalizationRequired ErrorCode = "normalization_required"
	// CodeMalformed reports an invalid UTF-8, empty, whitespace, or control entry.
	CodeMalformed ErrorCode = "malformed"
	// CodeMetadata reports missing or inconsistent provenance metadata.
	CodeMetadata ErrorCode = "metadata"
	// CodeChecksum reports an embedded-content checksum mismatch.
	CodeChecksum ErrorCode = "checksum"
	// CodePrefixCollision reports entries with a prohibited common prefix.
	CodePrefixCollision ErrorCode = "prefix_collision"
	// CodeOversized reports a list above resource limits.
	CodeOversized ErrorCode = "oversized"
	// CodeInvalidOption reports an invalid validation option.
	CodeInvalidOption ErrorCode = "invalid_option"
)

// Error intentionally excludes words and other potentially sensitive input.
type Error struct {
	Code ErrorCode
}

func (e *Error) Error() string {
	return fmt.Sprintf("wordlist: validation failed (%s)", e.Code)
}

// Metadata pins provenance and integrity for a list.
type Metadata struct {
	ID            string
	Language      string
	Source        string
	Version       string
	License       string
	ExpectedWords int
	SHA256        string
	SourceSHA256  string
}

// List is an immutable validated word list.
type List struct {
	metadata Metadata
	words    []string
	indices  map[string]int
}

type config struct {
	uniquePrefix int
	requireNFKD  bool
}

// WithNFKD requires every entry to already use BIP-39-compatible NFKD.
func WithNFKD() Option {
	return func(configuration *config) error {
		configuration.requireNFKD = true
		return nil
	}
}

// Option configures list validation.
type Option func(*config) error

// WithUniquePrefix rejects entries sharing the first length Unicode code
// points. It is useful for formats that permit abbreviated words.
func WithUniquePrefix(length int) Option {
	return func(configuration *config) error {
		if length <= 0 {
			return &Error{Code: CodeInvalidOption}
		}
		configuration.uniquePrefix = length
		return nil
	}
}

// New validates words and returns an immutable copy.
func New(metadata Metadata, words []string, options ...Option) (*List, error) {
	if len(words) == 0 {
		return nil, &Error{Code: CodeEmpty}
	}
	if len(words) > maxWords {
		return nil, &Error{Code: CodeOversized}
	}
	if !validMetadata(metadata) || metadata.ExpectedWords != len(words) {
		return nil, &Error{Code: CodeMetadata}
	}

	configuration := config{}
	for _, option := range options {
		if option == nil {
			return nil, &Error{Code: CodeInvalidOption}
		}
		if err := option(&configuration); err != nil {
			return nil, err
		}
	}

	copyOfWords := append([]string(nil), words...)
	indices := make(map[string]int, len(copyOfWords))
	normalized := make(map[string]struct{}, len(copyOfWords))
	prefixes := make(map[string]struct{}, len(copyOfWords))
	for index, word := range copyOfWords {
		if !validWord(word) {
			return nil, &Error{Code: CodeMalformed}
		}
		if configuration.requireNFKD && !norm.NFKD.IsNormalString(word) {
			return nil, &Error{Code: CodeNormalizationRequired}
		}
		if _, exists := indices[word]; exists {
			return nil, &Error{Code: CodeDuplicate}
		}

		normalizedWord := norm.NFKD.String(word)
		if _, exists := normalized[normalizedWord]; exists {
			return nil, &Error{Code: CodeNormalizationCollision}
		}
		indices[word] = index
		normalized[normalizedWord] = struct{}{}

		if configuration.uniquePrefix > 0 {
			prefix := runePrefix(normalizedWord, configuration.uniquePrefix)
			if _, exists := prefixes[prefix]; exists {
				return nil, &Error{Code: CodePrefixCollision}
			}
			prefixes[prefix] = struct{}{}
		}
	}

	if !strings.EqualFold(metadata.SHA256, Checksum(copyOfWords)) {
		return nil, &Error{Code: CodeChecksum}
	}

	return &List{metadata: metadata, words: copyOfWords, indices: indices}, nil
}

// Checksum returns the SHA-256 checksum of newline-terminated UTF-8 entries.
func Checksum(words []string) string {
	hash := sha256.New()
	for _, word := range words {
		_, _ = hash.Write([]byte(word))
		_, _ = hash.Write([]byte{'\n'})
	}

	return hex.EncodeToString(hash.Sum(nil))
}

// Len returns the number of entries.
func (l *List) Len() int {
	return len(l.words)
}

// Word returns the entry at index.
func (l *List) Word(index int) (string, bool) {
	if index < 0 || index >= len(l.words) {
		return "", false
	}

	return l.words[index], true
}

// Index returns the exact entry index.
func (l *List) Index(word string) (int, bool) {
	index, exists := l.indices[word]
	return index, exists
}

// Words returns a copy of all entries.
func (l *List) Words() []string {
	return append([]string(nil), l.words...)
}

// Metadata returns the pinned list metadata.
func (l *List) Metadata() Metadata {
	return l.metadata
}

func validMetadata(metadata Metadata) bool {
	return metadata.ID != "" && metadata.Language != "" && metadata.Source != "" &&
		metadata.Version != "" && metadata.License != "" && metadata.ExpectedWords > 0 &&
		validChecksum(metadata.SHA256) && validChecksum(metadata.SourceSHA256)
}

func validChecksum(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func validWord(word string) bool {
	if word == "" || len(word) > maxWordSize || !utf8.ValidString(word) {
		return false
	}
	for _, character := range word {
		if unicode.IsSpace(character) || unicode.IsControl(character) {
			return false
		}
	}

	return true
}

func runePrefix(value string, length int) string {
	runes := []rune(norm.NFC.String(value))
	if len(runes) <= length {
		return string(runes)
	}
	return string(runes[:length])
}
