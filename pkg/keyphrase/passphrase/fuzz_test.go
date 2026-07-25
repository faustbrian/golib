package passphrase_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/keyphrase/passphrase"
	"github.com/faustbrian/golib/pkg/keyphrase/wordlist"
)

func FuzzParsing(f *testing.F) {
	words := []string{"alpha", "bravo", "charlie"}
	checksum := wordlist.Checksum(words)
	list, err := wordlist.New(wordlist.Metadata{
		ID: "fuzz", Language: "en", Source: "fuzz", Version: "1",
		License: "CC0-1.0", ExpectedWords: len(words), SHA256: checksum,
		SourceSHA256: checksum,
	}, words)
	if err != nil {
		f.Fatalf("wordlist.New() error = %v", err)
	}
	policy := passphrase.Policy{WordList: list, Words: 3, Separator: "-"}
	f.Add([]byte("alpha-bravo-charlie"))
	f.Add([]byte("alpha--charlie"))
	f.Fuzz(func(t *testing.T, encoded []byte) {
		if len(encoded) > 128<<10 {
			t.Skip()
		}
		parsed, err := passphrase.Parse(encoded, policy)
		if err != nil {
			return
		}
		if len(parsed.Words) != policy.Words {
			t.Fatal("parsed word count mismatch")
		}
	})
}
