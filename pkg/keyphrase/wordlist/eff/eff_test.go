package eff_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/keyphrase/wordlist/eff"
)

func TestEmbeddedListsMatchPinnedMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		load   func() (*eff.List, error)
		length int
		sha256 string
	}{
		{name: "large", load: eff.Large, length: 7776, sha256: "6d557f0693958fb5e650b68b5bee585eb82cf4da32965505c789e924743bc522"},
		{name: "short one", load: eff.ShortOne, length: 1296, sha256: "36ecca49e4fa20ca84b176c32f2e9c82f98f446585190e75f9879a95c08247bf"},
		{name: "short two", load: eff.ShortTwo, length: 1296, sha256: "7aa57a4d3ecf6581729992bad9575bacdebf7c28378af2aec6a50f11aec326f5"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			list, err := test.load()
			if err != nil {
				t.Fatalf("load() error = %v", err)
			}
			if list.Len() != test.length {
				t.Fatalf("length = %d, want %d", list.Len(), test.length)
			}
			metadata := list.Metadata()
			if metadata.SHA256 != test.sha256 || metadata.Source == "" || metadata.SourceSHA256 == "" || metadata.License == "" {
				t.Fatalf("incomplete or incorrect metadata: %#v", metadata)
			}
		})
	}
}
