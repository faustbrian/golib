package filesystem

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
)

func FuzzRootRecordCanonicalRoundTrip(f *testing.F) {
	f.Add(encodeRootRecord(mpt.EmptyRoot()))
	f.Add([]byte("malformed"))

	f.Fuzz(func(t *testing.T, record []byte) {
		if len(record) > rootRecordLen+1 {
			return
		}
		root, err := decodeRootRecord(record)
		if err != nil {
			return
		}
		roundTrip := encodeRootRecord(root)
		if !bytes.Equal(roundTrip, record) {
			t.Fatalf("root record round trip = %x, want %x", roundTrip, record)
		}
	})
}

func FuzzNodeFilenameCanonicalDecode(f *testing.F) {
	f.Add(strings.Repeat("00", mpt.RootBytes))
	f.Add(strings.Repeat("AA", mpt.RootBytes))
	f.Add("malformed")

	f.Fuzz(func(t *testing.T, name string) {
		if len(name) > hex.EncodedLen(mpt.RootBytes)+1 {
			return
		}
		root, err := decodeNodeName(name)
		if err != nil {
			return
		}
		if encoded := hex.EncodeToString(root[:]); encoded != name {
			t.Fatalf("canonical node filename = %q, want %q", encoded, name)
		}
	})
}
