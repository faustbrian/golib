package authstate

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
)

const (
	statelessFuzzEntryBytes  = 4
	statelessFuzzUpdateBytes = 5
)

func FuzzStatelessUpdaterMatchesStatefulTransition(f *testing.F) {
	f.Add(
		[]byte{0x00, 0x00, 0x00, 0x11, 0x00, 0x00, 0x01, 0x22},
		[]byte{0x01, 0x00, 0x00, 0x00, 0x00},
	)
	f.Add(
		[]byte{0x30, 0x10, 0x01, 0x11},
		[]byte{0x01, 0x30, 0x10, 0x01, 0x00},
	)
	f.Add(
		[]byte{0x00, 0x00, 0x00, 0x11},
		[]byte{0x00, 0x01, 0x00, 0x01, 0x22},
	)
	f.Add(
		[]byte{0x20, 0x10, 0x00, 0x11, 0x20, 0x20, 0x00, 0x22},
		[]byte{
			0x01, 0x20, 0x10, 0x00, 0x00,
			0x00, 0x20, 0x30, 0x00, 0x33,
		},
	)
	f.Add([]byte(nil), []byte{0x00, 0x40, 0x00, 0x00, 0x44})

	openingLimits := testAuthstateAggregateOpeningLimits()
	openingLimits.MaxQueries = 4_096
	openingLimits.MaxScalarDecodes = 4_096 * backend.VectorWidth
	openingLimits.MaxMSMTerms = 8_192 * backend.VectorWidth
	proofEngine, err := NewProofEngine(context.Background(), openingLimits)
	if err != nil {
		f.Fatalf("new fuzz proof engine: %v", err)
	}
	updater, err := NewStatelessUpdater(
		context.Background(), openingLimits, testCommitmentLimits(),
	)
	if err != nil {
		f.Fatalf("new fuzz stateless updater: %v", err)
	}

	f.Fuzz(func(t *testing.T, encodedEntries []byte, encodedUpdates []byte) {
		if len(encodedEntries) > 4*statelessFuzzEntryBytes ||
			len(encodedUpdates) > 2*statelessFuzzUpdateBytes {
			return
		}
		updates := decodeStatelessFuzzUpdates(encodedUpdates)
		if len(updates) == 0 {
			return
		}
		snapshot := newTestSnapshot(t, decodeStatelessFuzzEntries(encodedEntries))
		proof, err := proofEngine.ProveUpdates(
			context.Background(), snapshot, updates, topologyProofGenerationLimits(),
		)
		if err != nil {
			t.Fatalf("prove fuzz transition: %v", err)
		}
		got, err := updater.Apply(
			context.Background(), proof, updates,
			topologyProofVerificationLimits(), topologyStatelessUpdateLimits(),
		)
		if err != nil {
			t.Fatalf("apply stateless fuzz transition: %v", err)
		}
		wantSnapshot, _, err := snapshot.Apply(context.Background(), updates)
		if err != nil {
			t.Fatalf("apply stateful fuzz transition: %v", err)
		}
		want, err := wantSnapshot.RootContainer(context.Background())
		if err != nil {
			t.Fatalf("read stateful fuzz root: %v", err)
		}
		assertSameBackendRoot(t, got, want)
	})
}

func decodeStatelessFuzzEntries(encoded []byte) []Entry {
	byKey := make(map[Key]Value, len(encoded)/statelessFuzzEntryBytes)
	for len(encoded) >= statelessFuzzEntryBytes {
		key := statelessFuzzKey(encoded[0], encoded[1], encoded[2])
		byKey[key] = testValue(encoded[3])
		encoded = encoded[statelessFuzzEntryBytes:]
	}
	entries := make([]Entry, 0, len(byKey))
	for key, value := range byKey {
		entries = append(entries, Entry{Key: key, Value: value})
	}

	return entries
}

func decodeStatelessFuzzUpdates(encoded []byte) []Update {
	byKey := make(map[Key]Update, len(encoded)/statelessFuzzUpdateBytes)
	for len(encoded) >= statelessFuzzUpdateBytes {
		key := statelessFuzzKey(encoded[1], encoded[2], encoded[3])
		if encoded[0]&1 == 0 {
			byKey[key] = Set(key, testValue(encoded[4]))
		} else {
			byKey[key] = Delete(key)
		}
		encoded = encoded[statelessFuzzUpdateBytes:]
	}
	updates := make([]Update, 0, len(byKey))
	for _, update := range byKey {
		updates = append(updates, update)
	}

	return updates
}

func statelessFuzzKey(first byte, second byte, suffix byte) Key {
	var key Key
	key[0] = first
	key[1] = second
	key[31] = suffix

	return key
}
