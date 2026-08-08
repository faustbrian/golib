package verkletree_test

import (
	"bytes"
	"context"
	"testing"

	verkletree "github.com/faustbrian/golib/pkg/verkle-tree"
)

const (
	publicStatelessFuzzEntryBytes  = 5
	publicStatelessFuzzUpdateBytes = 6
)

func FuzzPublicStatelessTransitionMatchesStatefulSnapshot(f *testing.F) {
	f.Add(
		[]byte{0x20, 0x10, 0x10, 0x00, 0x11, 0x20, 0x40, 0x00, 0x00, 0x22},
		[]byte{
			0x01, 0x20, 0x10, 0x10, 0x00, 0x00,
			0x00, 0x20, 0x10, 0x30, 0x00, 0x33,
			0x01, 0x20, 0x40, 0x00, 0x00, 0x00,
		},
	)
	f.Add(
		[]byte{0x10, 0x00, 0x00, 0x01, 0x11, 0x10, 0x00, 0x00, 0x02, 0x22},
		[]byte{0x01, 0x10, 0x00, 0x00, 0x01, 0x00},
	)
	f.Add(
		[]byte{0x30, 0x10, 0x00, 0x00, 0x11},
		[]byte{
			0x01, 0x30, 0x10, 0x00, 0x00, 0x00,
			0x00, 0x30, 0x30, 0x00, 0x00, 0x33,
		},
	)
	f.Add([]byte(nil), []byte{0x00, 0x40, 0x00, 0x00, 0x00, 0x44})

	openingLimits := publicTopologyOpeningLimits()
	proofEngine, err := verkletree.NewProofEngine(
		context.Background(),
		verkletree.BandersnatchIPA256V0(),
		openingLimits,
	)
	if err != nil {
		f.Fatalf("new public fuzz proof engine: %v", err)
	}
	statelessEngine, err := verkletree.NewStatelessEngine(
		context.Background(),
		verkletree.BandersnatchIPA256V0(),
		openingLimits,
		publicSnapshotLimits().Commitment,
	)
	if err != nil {
		f.Fatalf("new public fuzz stateless engine: %v", err)
	}

	f.Fuzz(func(t *testing.T, encodedEntries []byte, encodedUpdates []byte) {
		if len(encodedEntries) > 4*publicStatelessFuzzEntryBytes ||
			len(encodedUpdates) > 3*publicStatelessFuzzUpdateBytes {
			return
		}
		updates := decodePublicStatelessFuzzUpdates(encodedUpdates)
		if len(updates) == 0 {
			return
		}
		snapshot, err := verkletree.NewSnapshot(
			context.Background(),
			verkletree.BandersnatchIPA256V0(),
			decodePublicStatelessFuzzEntries(encodedEntries),
			publicSnapshotLimits(),
		)
		if err != nil {
			t.Fatalf("new public fuzz snapshot: %v", err)
		}
		proof, err := proofEngine.ProveUpdates(
			context.Background(), snapshot, updates,
			publicTopologyProofGenerationLimits(),
		)
		if err != nil {
			t.Fatalf("prove public fuzz updates: %v", err)
		}
		next, _, err := snapshot.Apply(context.Background(), updates)
		if err != nil {
			t.Fatalf("apply public fuzz stateful updates: %v", err)
		}
		postRoot, err := next.Root()
		if err != nil {
			t.Fatalf("read public fuzz stateful root: %v", err)
		}
		witness, err := verkletree.NewWitness(
			context.Background(), proof, updates, postRoot, publicWitnessLimits(),
		)
		if err != nil {
			t.Fatalf("construct public fuzz witness: %v", err)
		}
		encoded, err := witness.Bytes(
			context.Background(), publicTopologyWitnessEncodingLimits(),
		)
		if err != nil {
			t.Fatalf("encode public fuzz witness: %v", err)
		}
		decoded, err := verkletree.DecodeWitness(
			context.Background(), encoded, publicTopologyWitnessDecodingLimits(),
		)
		if err != nil {
			t.Fatalf("decode public fuzz witness: %v", err)
		}
		reencoded, err := decoded.Bytes(
			context.Background(), publicTopologyWitnessEncodingLimits(),
		)
		if err != nil {
			t.Fatalf("re-encode public fuzz witness: %v", err)
		}
		if !bytes.Equal(reencoded, encoded) {
			t.Fatal("public fuzz witness did not round-trip canonically")
		}
		wantPreRoot, err := snapshot.Root()
		if err != nil {
			t.Fatalf("read public fuzz snapshot root: %v", err)
		}
		result, err := statelessEngine.ApplyForRoot(
			context.Background(), decoded, wantPreRoot,
			publicTopologyProofVerificationLimits(),
			publicTopologyStatelessUpdateLimits(),
		)
		if err != nil {
			t.Fatalf("apply public fuzz witness: %v", err)
		}
		preRoot, err := result.PreRoot()
		if err != nil {
			t.Fatalf("read public fuzz pre-root: %v", err)
		}
		assertPublicRootsEqual(t, preRoot, wantPreRoot)
		gotPostRoot, err := result.PostRoot()
		if err != nil {
			t.Fatalf("read public fuzz post-root: %v", err)
		}
		assertPublicRootsEqual(t, gotPostRoot, postRoot)
	})
}

func decodePublicStatelessFuzzEntries(encoded []byte) []verkletree.Entry {
	byKey := make(map[verkletree.Key]verkletree.Value, len(encoded)/publicStatelessFuzzEntryBytes)
	for len(encoded) >= publicStatelessFuzzEntryBytes {
		key := publicStatelessFuzzKey(encoded[0], encoded[1], encoded[2], encoded[3])
		byKey[key] = publicValue(encoded[4])
		encoded = encoded[publicStatelessFuzzEntryBytes:]
	}
	entries := make([]verkletree.Entry, 0, len(byKey))
	for key, value := range byKey {
		entries = append(entries, verkletree.Entry{Key: key, Value: value})
	}

	return entries
}

func decodePublicStatelessFuzzUpdates(encoded []byte) []verkletree.Update {
	byKey := make(map[verkletree.Key]verkletree.Update, len(encoded)/publicStatelessFuzzUpdateBytes)
	for len(encoded) >= publicStatelessFuzzUpdateBytes {
		key := publicStatelessFuzzKey(encoded[1], encoded[2], encoded[3], encoded[4])
		if encoded[0]&1 == 0 {
			byKey[key] = verkletree.Set(key, publicValue(encoded[5]))
		} else {
			byKey[key] = verkletree.Delete(key)
		}
		encoded = encoded[publicStatelessFuzzUpdateBytes:]
	}
	updates := make([]verkletree.Update, 0, len(byKey))
	for _, update := range byKey {
		updates = append(updates, update)
	}

	return updates
}

func publicStatelessFuzzKey(first, second, third, suffix byte) verkletree.Key {
	var key verkletree.Key
	key[0] = first
	key[1] = second
	key[2] = third
	key[31] = suffix

	return key
}

func publicTopologyOpeningLimits() verkletree.OpeningLimits {
	limits := publicOpeningLimits()
	limits.MaxQueries = 4_096
	limits.MaxScalarDecodes = 4_096 * 256
	limits.MaxMSMTerms = 8_192 * 256

	return limits
}

func publicTopologyWitnessDecodingLimits() verkletree.WitnessDecodingLimits {
	limits := publicWitnessDecodingLimits()
	limits.MaxWitnessBytes = 8 << 20
	limits.Proof.MaxProofBytes = 4 << 20
	limits.Proof.MaxClaims = 1_024
	limits.Proof.MaxStemPaths = 1_024
	limits.Proof.MaxPathCommitments = 32_768
	limits.Proof.MaxPathDerivations = 32_768
	limits.Proof.MaxPathBytes = 1 << 20
	limits.Proof.MaxPointDecodes = 32_768
	limits.Proof.MaxTemporaryBytes = 64 << 20

	return limits
}

func publicTopologyWitnessEncodingLimits() verkletree.WitnessEncodingLimits {
	limits := publicWitnessEncodingLimits()
	limits.MaxWitnessBytes = 8 << 20
	limits.MaxProofBytes = 4 << 20
	limits.MaxTemporaryBytes = 4 << 20

	return limits
}
