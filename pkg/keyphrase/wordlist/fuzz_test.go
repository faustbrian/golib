package wordlist_test

import (
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/keyphrase/wordlist"
)

func FuzzValidation(f *testing.F) {
	f.Add("alpha\nbravo\ncharlie")
	f.Add("é\ne\u0301")
	f.Fuzz(func(t *testing.T, encoded string) {
		if len(encoded) > 16<<10 {
			t.Skip()
		}
		words := strings.Split(encoded, "\n")
		checksum := wordlist.Checksum(words)
		list, err := wordlist.New(wordlist.Metadata{
			ID:            "fuzz",
			Language:      "und",
			Source:        "fuzz",
			Version:       "1",
			License:       "CC0-1.0",
			ExpectedWords: len(words),
			SHA256:        checksum,
			SourceSHA256:  checksum,
		}, words)
		if err != nil {
			return
		}
		if list.Len() != len(words) || wordlist.Checksum(list.Words()) != checksum {
			t.Fatal("validated list failed round trip")
		}
	})
}
