package verkletree

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"
)

func FuzzLoadSnapshotRootNode(f *testing.F) {
	snapshot := testStorageReadSnapshot(f)
	seedReader := internalReaderFromSnapshot(f, snapshot)
	rootID, err := seedReader.view.publication.RootNode()
	if err != nil {
		f.Fatalf("read seed root-node ID: %v", err)
	}
	valid := bytes.Clone(seedReader.view.nodes[rootID])
	f.Add(valid)
	f.Add([]byte(nil))
	f.Add(valid[:len(valid)-1])

	f.Fuzz(func(t *testing.T, candidate []byte) {
		const maxFuzzNodeBytes = 4 << 10
		if len(candidate) > maxFuzzNodeBytes {
			return
		}
		original := bytes.Clone(candidate)
		reader := cloneInternalStorageReader(seedReader)
		oldRootID, rootErr := reader.view.publication.RootNode()
		if rootErr != nil {
			t.Fatalf("read root-node ID: %v", rootErr)
		}
		newRootID := NodeID(sha256.Sum256(candidate))
		delete(reader.view.nodes, oldRootID)
		reader.view.nodes[newRootID] = candidate
		reader.view.publication.rootNode = newRootID
		reader.view.aliasReads = true

		loaded, loadErr := LoadSnapshot(
			context.Background(),
			BandersnatchIPA256V0(),
			reader,
			testInternalStorageReadLimits(),
		)
		if reader.view.closeCalls != 1 {
			t.Fatalf("read view close calls = %d", reader.view.closeCalls)
		}
		if !bytes.Equal(candidate, original) {
			t.Fatal("snapshot load mutated caller-owned node bytes")
		}
		if loadErr != nil {
			if bytes.Equal(candidate, valid) {
				t.Fatalf("load valid root node: %v", loadErr)
			}
			return
		}
		gotRoot, gotRootErr := loaded.Root()
		wantRoot, wantRootErr := snapshot.Root()
		if gotRootErr != nil || wantRootErr != nil {
			t.Fatalf("read accepted roots: %v / %v", gotRootErr, wantRootErr)
		}
		if !rootsEqualForStorageTest(t, gotRoot, wantRoot) {
			t.Fatal("accepted root node reconstructed a different published root")
		}
	})
}
