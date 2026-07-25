package specification_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/specification"
)

func TestReadReturnsPinnedCallerOwnedResources(t *testing.T) {
	t.Parallel()

	first, err := specification.Read("schemas/3.2/dialect-2025-09-17.json")
	if err != nil {
		t.Fatal(err)
	}
	second, err := specification.Read("schemas/3.2/dialect-2025-09-17.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("successive reads differ")
	}
	first[0] ^= 0xff
	if bytes.Equal(first, second) {
		t.Fatal("Read exposed embedded storage")
	}
	if _, err := specification.Read("schemas/missing.json"); err == nil {
		t.Fatal("missing resource error = nil")
	}
}

func TestPinnedIANARegistryRetainsItsAuthoritativeBytes(t *testing.T) {
	t.Parallel()

	registries := map[string]string{
		"registries/iana/http-status-codes-1.csv": "4a9550d4b4ae49cf41cf9050cf9b56b0d6082ad1edfc0e2b09b07f251a36d7a4",
		"registries/iana/authschemes.csv":         "9624ab05b6d91d0658f16e082699b8d68a09d16d06c9c030409bebe92cc58347",
	}
	for name, want := range registries {
		data, err := specification.Read(name)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		if got := hex.EncodeToString(digest[:]); got != want {
			t.Errorf("%s SHA-256 = %s, want %s", name, got, want)
		}
	}
}
