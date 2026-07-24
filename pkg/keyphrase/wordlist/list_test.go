package wordlist_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/keyphrase/wordlist"
)

func TestNewCreatesImmutableIndexedList(t *testing.T) {
	t.Parallel()

	words := []string{"alpha", "bravo", "charlie"}
	metadata := wordlist.Metadata{
		ID:            "test",
		Language:      "en",
		Source:        "https://example.test/list",
		Version:       "1",
		License:       "CC0-1.0",
		ExpectedWords: 3,
		SHA256:        wordlist.Checksum(words),
		SourceSHA256:  wordlist.Checksum(words),
	}

	list, err := wordlist.New(metadata, words, wordlist.WithUniquePrefix(2))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	words[0] = "changed"

	if got, ok := list.Word(0); !ok || got != "alpha" {
		t.Fatalf("Word(0) = %q, %t, want alpha, true", got, ok)
	}
	if got, ok := list.Index("charlie"); !ok || got != 2 {
		t.Fatalf("Index(charlie) = %d, %t, want 2, true", got, ok)
	}
	copyOfWords := list.Words()
	copyOfWords[0] = "changed again"
	if got, _ := list.Word(0); got != "alpha" {
		t.Fatalf("Word(0) after copy mutation = %q, want alpha", got)
	}
	if list.Len() != 3 || list.Metadata() != metadata {
		t.Fatal("list did not preserve length and metadata")
	}
}

func TestNewRejectsUnsafeLists(t *testing.T) {
	t.Parallel()

	invalidContentChecksum := metadataFor([]string{"one"})
	invalidContentChecksum.SHA256 = strings.Repeat("z", 64)
	invalidSourceChecksum := metadataFor([]string{"one"})
	invalidSourceChecksum.SourceSHA256 = strings.Repeat("z", 64)
	tests := []struct {
		name     string
		metadata wordlist.Metadata
		words    []string
		options  []wordlist.Option
		code     wordlist.ErrorCode
	}{
		{name: "empty", metadata: metadataFor(nil), code: wordlist.CodeEmpty},
		{name: "duplicate", metadata: metadataFor([]string{"same", "same"}), words: []string{"same", "same"}, code: wordlist.CodeDuplicate},
		{name: "normalization collision", metadata: metadataFor([]string{"é", "e\u0301"}), words: []string{"é", "e\u0301"}, code: wordlist.CodeNormalizationCollision},
		{name: "normalization required", metadata: metadataFor([]string{"é"}), words: []string{"é"}, options: []wordlist.Option{wordlist.WithNFKD()}, code: wordlist.CodeNormalizationRequired},
		{name: "whitespace", metadata: metadataFor([]string{"two words"}), words: []string{"two words"}, code: wordlist.CodeMalformed},
		{name: "metadata length", metadata: metadataFor([]string{"one"}), words: []string{"one", "two"}, code: wordlist.CodeMetadata},
		{name: "malformed content checksum", metadata: invalidContentChecksum, words: []string{"one"}, code: wordlist.CodeMetadata},
		{name: "malformed source checksum", metadata: invalidSourceChecksum, words: []string{"one"}, code: wordlist.CodeMetadata},
		{name: "checksum", metadata: wordlist.Metadata{ID: "test", Language: "en", Source: "source", Version: "1", License: "MIT", ExpectedWords: 1, SHA256: "0000000000000000000000000000000000000000000000000000000000000000", SourceSHA256: wordlist.Checksum([]string{"one"})}, words: []string{"one"}, code: wordlist.CodeChecksum},
		{name: "prefix", metadata: metadataFor([]string{"alpha", "alpine"}), words: []string{"alpha", "alpine"}, options: []wordlist.Option{wordlist.WithUniquePrefix(2)}, code: wordlist.CodePrefixCollision},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := wordlist.New(test.metadata, test.words, test.options...)
			var listError *wordlist.Error
			if !errors.As(err, &listError) {
				t.Fatalf("New() error = %v, want *wordlist.Error", err)
			}
			if listError.Code != test.code {
				t.Fatalf("error code = %q, want %q", listError.Code, test.code)
			}
		})
	}
}

func metadataFor(words []string) wordlist.Metadata {
	return wordlist.Metadata{
		ID:            "test",
		Language:      "en",
		Source:        "source",
		Version:       "1",
		License:       "MIT",
		ExpectedWords: len(words),
		SHA256:        wordlist.Checksum(words),
		SourceSHA256:  wordlist.Checksum(words),
	}
}
