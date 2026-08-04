package verkletree_test

import (
	"bytes"
	"context"
	"testing"

	verkletree "github.com/faustbrian/golib/pkg/verkle-tree"
)

func FuzzDecodeSnapshot(f *testing.F) {
	for _, entries := range [][]verkletree.Entry{
		nil,
		{{Key: publicKey(0x10, 0x01), Value: publicValue(1)}},
		{
			{Key: publicKey(0x10, 0x01), Value: publicValue(1)},
			{Key: publicKey(0x20, 0x02), Value: publicValue(2)},
		},
	} {
		snapshot, err := verkletree.NewSnapshot(
			context.Background(),
			verkletree.BandersnatchIPA256V0(),
			entries,
			publicSnapshotLimits(),
		)
		if err != nil {
			f.Fatalf("new seed snapshot: %v", err)
		}
		encoded, err := snapshot.Bytes(
			context.Background(), publicSnapshotEncodingLimits(),
		)
		if err != nil {
			f.Fatalf("encode seed snapshot: %v", err)
		}
		f.Add(encoded)
	}

	f.Fuzz(func(t *testing.T, encoded []byte) {
		decoded, err := verkletree.DecodeSnapshot(
			context.Background(), encoded, publicSnapshotDecodingLimits(),
		)
		if err != nil {
			return
		}
		reencoded, err := decoded.Bytes(
			context.Background(), publicSnapshotEncodingLimits(),
		)
		if err != nil {
			t.Fatalf("reencode accepted snapshot: %v", err)
		}
		if !bytes.Equal(reencoded, encoded) {
			t.Fatal("accepted snapshot has an alternate encoding")
		}
	})
}
