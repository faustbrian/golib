package mpt

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/internal/rlp"
)

func TestDecodeStorageWordRejectsMalformedAndNonCanonicalValues(t *testing.T) {
	t.Parallel()

	invalidLimits := DefaultLimits()
	invalidLimits.MaxNodeReads = 0
	if _, err := decodeStorageWord(nil, invalidLimits); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("decodeStorageWord(invalid limits) error = %v", err)
	}

	list, err := rlp.Encode(rlp.List(), rlp.DefaultLimits())
	if err != nil {
		t.Fatalf("encode list: %v", err)
	}
	oversized, err := rlp.Encode(
		rlp.String(make([]byte, RootBytes+1)), rlp.DefaultLimits(),
	)
	if err != nil {
		t.Fatalf("encode oversized: %v", err)
	}
	for name, encoded := range map[string][]byte{
		"malformed":        {0x81},
		"list":             list,
		"encoded zero":     {0x80},
		"leading zero":     {0x00},
		"oversized word":   oversized,
		"non-minimal zero": {0x81, 0x00},
	} {
		if _, err := decodeStorageWord(
			encoded, DefaultLimits(),
		); !errors.Is(err, ErrInvalidStorageValue) {
			t.Fatalf("%s decodeStorageWord() error = %v", name, err)
		}
	}
}

func TestDecodeAccountRejectsNonceBeyondUint64(t *testing.T) {
	t.Parallel()

	codeHash := EmptyCodeHash()
	encoded, err := rlp.Encode(rlp.List(
		rlp.String([]byte{1, 0, 0, 0, 0, 0, 0, 0, 0}),
		rlp.String(nil),
		rlp.String(EmptyRoot().Bytes()),
		rlp.String(codeHash[:]),
	), rlp.DefaultLimits())
	if err != nil {
		t.Fatalf("encode account: %v", err)
	}
	if _, err := decodeAccount(encoded, DefaultLimits()); !errors.Is(err, ErrInvalidAccount) {
		t.Fatalf("decodeAccount(overflow nonce) error = %v", err)
	}
}

func TestEthereumProfileConstantsAndIntegerBoundaries(t *testing.T) {
	t.Parallel()

	if EmptyCodeHash() != [RootBytes]byte(keccakRoot(nil)) {
		t.Fatalf("EmptyCodeHash() = %x", EmptyCodeHash())
	}
	if got := minimalUint64(^uint64(0)); len(got) != 8 {
		t.Fatalf("minimalUint64(max) = %x", got)
	} else {
		for _, octet := range got {
			if octet != 0xff {
				t.Fatalf("minimalUint64(max) = %x", got)
			}
		}
	}
}
