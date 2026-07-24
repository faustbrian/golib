package wordlist

import (
	"errors"
	"strings"
	"testing"
)

func TestValidationResourceAndOptionFailures(t *testing.T) {
	t.Parallel()

	listError := &Error{Code: CodeMalformed}
	if listError.Error() != "wordlist: validation failed (malformed)" {
		t.Fatal("Error() contract mismatch")
	}

	oversized := make([]string, maxWords+1)
	if _, err := New(Metadata{}, oversized); errorCode(err) != CodeOversized {
		t.Fatalf("New(oversized) code = %q", errorCode(err))
	}
	hostileCount := make([]string, (1<<16)+1)
	if _, err := New(Metadata{}, hostileCount); errorCode(err) != CodeOversized {
		t.Fatalf("New(hostile count) code = %q", errorCode(err))
	}
	words := []string{"valid"}
	metadata := metadataForInternal(words)
	if _, err := New(metadata, words, nil); errorCode(err) != CodeInvalidOption {
		t.Fatalf("New(nil option) code = %q", errorCode(err))
	}
	if _, err := New(metadata, words, WithUniquePrefix(0)); errorCode(err) != CodeInvalidOption {
		t.Fatalf("New(invalid prefix) code = %q", errorCode(err))
	}

	malformed := []string{""}
	if _, err := New(metadataForInternal(malformed), malformed); errorCode(err) != CodeMalformed {
		t.Fatalf("New(malformed) code = %q", errorCode(err))
	}
}

func TestListBoundaryAccessAndShortPrefix(t *testing.T) {
	t.Parallel()

	words := []string{"alpha", "bravo"}
	list, err := New(metadataForInternal(words), words)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if word, ok := list.Word(-1); ok || word != "" {
		t.Fatalf("Word(-1) = %q, %t", word, ok)
	}
	if word, ok := list.Word(list.Len()); ok || word != "" {
		t.Fatalf("Word(Len()) = %q, %t", word, ok)
	}
	if runePrefix("é", 4) != "é" {
		t.Fatal("short normalized prefix changed")
	}
	if runePrefix("éx", 2) != "éx" {
		t.Fatal("exact-length normalized prefix changed")
	}
}

func TestValidationBoundaries(t *testing.T) {
	t.Parallel()

	metadata := metadataForInternal([]string{"valid"})
	if !validMetadata(metadata) {
		t.Fatal("valid metadata was rejected")
	}
	metadata.ExpectedWords = 0
	if validMetadata(metadata) {
		t.Fatal("zero expected words was accepted")
	}

	if !validWord(strings.Repeat("a", maxWordSize)) {
		t.Fatal("maximum-size word was rejected")
	}
	if validWord(strings.Repeat("a", maxWordSize+1)) {
		t.Fatal("oversized word was accepted")
	}
}

func metadataForInternal(words []string) Metadata {
	checksum := Checksum(words)
	return Metadata{
		ID: "internal", Language: "und", Source: "internal", Version: "1",
		License: "CC0-1.0", ExpectedWords: len(words), SHA256: checksum,
		SourceSHA256: checksum,
	}
}

func errorCode(err error) ErrorCode {
	var listError *Error
	if errors.As(err, &listError) {
		return listError.Code
	}
	return ""
}
