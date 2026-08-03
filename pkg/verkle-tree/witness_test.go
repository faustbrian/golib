package verkletree_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	verkletree "github.com/faustbrian/golib/pkg/verkle-tree"
)

func TestPublicStatelessWitnessRoundTripAndApplication(t *testing.T) {
	t.Parallel()

	first := publicKey(0x10, 0x20)
	second := publicKey(0x10, 0x21)
	snapshot, err := verkletree.NewSnapshot(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		[]verkletree.Entry{{Key: first, Value: publicValue(1)}},
		publicSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("new snapshot: %v", err)
	}
	proofEngine, err := verkletree.NewProofEngine(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		publicOpeningLimits(),
	)
	if err != nil {
		t.Fatalf("new proof engine: %v", err)
	}
	proof, err := proofEngine.Prove(
		context.Background(),
		snapshot,
		[]verkletree.Key{second, first},
		publicProofGenerationLimits(),
	)
	if err != nil {
		t.Fatalf("prove update keys: %v", err)
	}
	updates := []verkletree.Update{
		verkletree.Set(second, publicValue(3)),
		verkletree.Set(first, publicValue(2)),
	}
	next, _, err := snapshot.Apply(context.Background(), updates)
	if err != nil {
		t.Fatalf("stateful apply: %v", err)
	}
	postRoot, err := next.Root()
	if err != nil {
		t.Fatalf("stateful post-state root: %v", err)
	}
	witness, err := verkletree.NewWitness(
		context.Background(),
		proof,
		updates,
		postRoot,
		publicWitnessLimits(),
	)
	if err != nil {
		t.Fatalf("new witness: %v", err)
	}
	boundProof, err := witness.Proof()
	if err != nil {
		t.Fatalf("witness proof: %v", err)
	}
	if err := proofEngine.Verify(
		context.Background(), boundProof, publicProofVerificationLimits(),
	); err != nil {
		t.Fatalf("verify witness proof: %v", err)
	}
	boundPostRoot, err := witness.PostRoot()
	if err != nil {
		t.Fatalf("witness post-state root: %v", err)
	}
	assertPublicRootsEqual(t, boundPostRoot, postRoot)
	encoded, err := witness.Bytes(context.Background(), publicWitnessEncodingLimits())
	if err != nil {
		t.Fatalf("encode witness: %v", err)
	}
	encodingLimits := publicWitnessEncodingLimits()
	encodingLimits.MaxProofBytes = 1
	if _, err := witness.Bytes(
		context.Background(), encodingLimits,
	); resourceOf(err) != verkletree.ResourceProofBytes {
		t.Fatalf("proof-byte encoding budget error = %v", err)
	}
	decoded, err := verkletree.DecodeWitness(
		context.Background(),
		encoded,
		publicWitnessDecodingLimits(),
	)
	if err != nil {
		t.Fatalf("decode witness: %v", err)
	}
	reencoded, err := decoded.Bytes(context.Background(), publicWitnessEncodingLimits())
	if err != nil {
		t.Fatalf("re-encode witness: %v", err)
	}
	if !bytes.Equal(reencoded, encoded) {
		t.Fatal("canonical witness bytes changed after round trip")
	}

	canonicalUpdates, err := decoded.Updates(context.Background())
	if err != nil {
		t.Fatalf("copy witness updates: %v", err)
	}
	if len(canonicalUpdates) != 2 {
		t.Fatalf("update count = %d, want 2", len(canonicalUpdates))
	}
	firstKind, _ := canonicalUpdates[0].Kind()
	firstKey, _ := canonicalUpdates[0].Key()
	firstValue, firstHasValue, _ := canonicalUpdates[0].Value()
	if firstKind != verkletree.UpdateSet || firstKey != first ||
		!firstHasValue || firstValue != publicValue(2) {
		t.Fatal("witness updates are not canonical and inspectable")
	}

	engine, err := verkletree.NewStatelessEngine(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		publicOpeningLimits(),
		publicSnapshotLimits().Commitment,
	)
	if err != nil {
		t.Fatalf("new stateless engine: %v", err)
	}
	result, err := engine.Apply(
		context.Background(),
		decoded,
		publicProofVerificationLimits(),
		publicStatelessUpdateLimits(),
	)
	if err != nil {
		t.Fatalf("apply witness: %v", err)
	}
	preRoot, err := result.PreRoot()
	if err != nil {
		t.Fatalf("result pre-state root: %v", err)
	}
	wantPreRoot, err := snapshot.Root()
	if err != nil {
		t.Fatalf("snapshot pre-state root: %v", err)
	}
	assertPublicRootsEqual(t, preRoot, wantPreRoot)
	resultPostRoot, err := result.PostRoot()
	if err != nil {
		t.Fatalf("result post-state root: %v", err)
	}
	assertPublicRootsEqual(t, resultPostRoot, postRoot)
}

func TestPublicStatelessWitnessAppliesTopologyChangingSets(t *testing.T) {
	t.Parallel()

	existing := publicKey(0x10, 0x00)
	existing[1] = 0x20
	neighbor := publicKey(0x30, 0xff)
	different := publicKey(0x10, 0x80)
	different[1] = 0x21
	missing := publicKey(0x20, 0x01)
	snapshot, err := verkletree.NewSnapshot(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		[]verkletree.Entry{
			{Key: existing, Value: publicValue(1)},
			{Key: neighbor, Value: publicValue(2)},
		},
		publicSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("new topology snapshot: %v", err)
	}
	proofEngine, err := verkletree.NewProofEngine(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		publicOpeningLimits(),
	)
	if err != nil {
		t.Fatalf("new proof engine: %v", err)
	}
	proof, err := proofEngine.Prove(
		context.Background(), snapshot,
		[]verkletree.Key{missing, different},
		publicProofGenerationLimits(),
	)
	if err != nil {
		t.Fatalf("prove topology-changing keys: %v", err)
	}
	updates := []verkletree.Update{
		verkletree.Set(missing, verkletree.Value{}),
		verkletree.Set(different, publicValue(3)),
	}
	next, _, err := snapshot.Apply(context.Background(), updates)
	if err != nil {
		t.Fatalf("stateful topology apply: %v", err)
	}
	postRoot, err := next.Root()
	if err != nil {
		t.Fatalf("stateful topology root: %v", err)
	}
	witness, err := verkletree.NewWitness(
		context.Background(), proof, updates, postRoot, publicWitnessLimits(),
	)
	if err != nil {
		t.Fatalf("new topology witness: %v", err)
	}
	engine, err := verkletree.NewStatelessEngine(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		publicOpeningLimits(),
		publicSnapshotLimits().Commitment,
	)
	if err != nil {
		t.Fatalf("new stateless engine: %v", err)
	}
	result, err := engine.Apply(
		context.Background(), witness,
		publicProofVerificationLimits(), publicStatelessUpdateLimits(),
	)
	if err != nil {
		t.Fatalf("apply topology witness: %v", err)
	}
	got, err := result.PostRoot()
	if err != nil {
		t.Fatalf("topology result root: %v", err)
	}
	assertPublicRootsEqual(t, got, postRoot)
}

func TestPublicStatelessWitnessAppliesDeletionWithoutTopologyChange(t *testing.T) {
	t.Parallel()

	deleted := publicKey(0x28, 0x01)
	retained := publicKey(0x28, 0x02)
	snapshot, err := verkletree.NewSnapshot(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		[]verkletree.Entry{
			{Key: deleted, Value: publicValue(1)},
			{Key: retained, Value: publicValue(2)},
		},
		publicSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("new deletion snapshot: %v", err)
	}
	proofEngine, err := verkletree.NewProofEngine(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		publicOpeningLimits(),
	)
	if err != nil {
		t.Fatalf("new proof engine: %v", err)
	}
	proof, err := proofEngine.Prove(
		context.Background(), snapshot,
		[]verkletree.Key{deleted, retained},
		publicProofGenerationLimits(),
	)
	if err != nil {
		t.Fatalf("prove deletion and retained member: %v", err)
	}
	updates := []verkletree.Update{verkletree.Delete(deleted)}
	next, _, err := snapshot.Apply(context.Background(), updates)
	if err != nil {
		t.Fatalf("stateful deletion: %v", err)
	}
	postRoot, err := next.Root()
	if err != nil {
		t.Fatalf("stateful deletion root: %v", err)
	}
	witness, err := verkletree.NewWitness(
		context.Background(), proof, updates, postRoot, publicWitnessLimits(),
	)
	if err != nil {
		t.Fatalf("new deletion witness: %v", err)
	}
	encoded, err := witness.Bytes(
		context.Background(), publicWitnessEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("encode deletion witness: %v", err)
	}
	decoded, err := verkletree.DecodeWitness(
		context.Background(), encoded, publicWitnessDecodingLimits(),
	)
	if err != nil {
		t.Fatalf("decode deletion witness: %v", err)
	}
	gotUpdates, err := decoded.Updates(context.Background())
	if err != nil || len(gotUpdates) != 1 {
		t.Fatalf("decoded deletion updates = (%v, %v)", gotUpdates, err)
	}
	kind, err := gotUpdates[0].Kind()
	if err != nil || kind != verkletree.UpdateDelete {
		t.Fatalf("decoded deletion kind = (%v, %v)", kind, err)
	}
	engine, err := verkletree.NewStatelessEngine(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		publicOpeningLimits(),
		publicSnapshotLimits().Commitment,
	)
	if err != nil {
		t.Fatalf("new stateless engine: %v", err)
	}
	result, err := engine.Apply(
		context.Background(), decoded,
		publicProofVerificationLimits(), publicStatelessUpdateLimits(),
	)
	if err != nil {
		t.Fatalf("apply deletion witness: %v", err)
	}
	gotRoot, err := result.PostRoot()
	if err != nil {
		t.Fatalf("deletion result root: %v", err)
	}
	assertPublicRootsEqual(t, gotRoot, postRoot)
}

func TestPublicProofEngineBuildsAndAppliesTopologyDeletionWitness(t *testing.T) {
	t.Parallel()

	deleted := publicKey(0x2a, 0x01)
	snapshot, err := verkletree.NewSnapshot(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		[]verkletree.Entry{{Key: deleted, Value: publicValue(1)}},
		publicSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("new topology deletion snapshot: %v", err)
	}
	proofEngine, err := verkletree.NewProofEngine(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		publicOpeningLimits(),
	)
	if err != nil {
		t.Fatalf("new topology deletion proof engine: %v", err)
	}
	updates := []verkletree.Update{verkletree.Delete(deleted)}
	proof, err := proofEngine.ProveUpdates(
		context.Background(), snapshot, updates,
		publicTopologyProofGenerationLimits(),
	)
	if err != nil {
		t.Fatalf("prove public topology deletion: %v", err)
	}
	next, _, err := snapshot.Apply(context.Background(), updates)
	if err != nil {
		t.Fatalf("apply public stateful topology deletion: %v", err)
	}
	postRoot, err := next.Root()
	if err != nil {
		t.Fatalf("public stateful topology deletion root: %v", err)
	}
	witness, err := verkletree.NewWitness(
		context.Background(), proof, updates, postRoot, publicWitnessLimits(),
	)
	if err != nil {
		t.Fatalf("new public topology deletion witness: %v", err)
	}
	engine, err := verkletree.NewStatelessEngine(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		publicOpeningLimits(),
		publicSnapshotLimits().Commitment,
	)
	if err != nil {
		t.Fatalf("new public topology deletion engine: %v", err)
	}
	result, err := engine.Apply(
		context.Background(), witness,
		publicTopologyProofVerificationLimits(),
		publicTopologyStatelessUpdateLimits(),
	)
	if err != nil {
		t.Fatalf("apply public topology deletion witness: %v", err)
	}
	got, err := result.PostRoot()
	if err != nil {
		t.Fatalf("public topology deletion result root: %v", err)
	}
	assertPublicRootsEqual(t, got, postRoot)
}

func TestPublicStatelessWitnessRejectsInvalidUseAndTampering(t *testing.T) {
	t.Parallel()

	var nilContext context.Context
	key := publicKey(0x30, 0x40)
	snapshot, err := verkletree.NewSnapshot(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		[]verkletree.Entry{{Key: key, Value: publicValue(1)}},
		publicSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("new snapshot: %v", err)
	}
	proofEngine, err := verkletree.NewProofEngine(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		publicOpeningLimits(),
	)
	if err != nil {
		t.Fatalf("new proof engine: %v", err)
	}
	proof, err := proofEngine.Prove(
		context.Background(), snapshot, []verkletree.Key{key},
		publicProofGenerationLimits(),
	)
	if err != nil {
		t.Fatalf("prove update key: %v", err)
	}
	updates := []verkletree.Update{verkletree.Set(key, publicValue(2))}
	next, _, err := snapshot.Apply(context.Background(), updates)
	if err != nil {
		t.Fatalf("stateful apply: %v", err)
	}
	postRoot, _ := next.Root()
	witness, err := verkletree.NewWitness(
		context.Background(), proof, updates, postRoot, publicWitnessLimits(),
	)
	if err != nil {
		t.Fatalf("new witness: %v", err)
	}
	encoded, err := witness.Bytes(context.Background(), publicWitnessEncodingLimits())
	if err != nil {
		t.Fatalf("encode witness: %v", err)
	}

	if _, err := verkletree.NewWitness(
		context.Background(), proof, nil, postRoot, publicWitnessLimits(),
	); !errors.Is(err, verkletree.ErrInvalidWitness) {
		t.Fatalf("empty update witness error = %v", err)
	}
	if _, err := verkletree.NewWitness(
		context.Background(), proof, []verkletree.Update{{}}, postRoot,
		publicWitnessLimits(),
	); !errors.Is(err, verkletree.ErrInvalidUpdate) {
		t.Fatalf("invalid update witness error = %v", err)
	}
	if _, err := verkletree.NewWitness(
		nilContext, proof, updates, postRoot, publicWitnessLimits(),
	); !errors.Is(err, verkletree.ErrInvalidContext) {
		t.Fatalf("nil witness context error = %v", err)
	}
	if _, err := verkletree.NewWitness(
		context.Background(), verkletree.Proof{}, updates, postRoot,
		publicWitnessLimits(),
	); !errors.Is(err, verkletree.ErrInvalidWitness) {
		t.Fatalf("zero proof witness error = %v", err)
	}
	if _, err := verkletree.NewWitness(
		context.Background(), proof, updates, verkletree.Root{},
		publicWitnessLimits(),
	); !errors.Is(err, verkletree.ErrInvalidWitness) {
		t.Fatalf("zero post-root witness error = %v", err)
	}
	if _, err := verkletree.NewWitness(
		context.Background(), proof, updates, postRoot, verkletree.WitnessLimits{},
	); !errors.Is(err, verkletree.ErrInvalidLimits) {
		t.Fatalf("invalid witness limits error = %v", err)
	}
	temporaryLimits := publicWitnessLimits()
	temporaryLimits.MaxTemporaryBytes = 1
	if _, err := verkletree.NewWitness(
		context.Background(), proof, updates, postRoot, temporaryLimits,
	); resourceOf(err) != verkletree.ResourceTemporaryBytes {
		t.Fatalf("witness temporary budget error = %v", err)
	}
	exactLimits := publicWitnessLimits()
	exactLimits.MaxUpdates = 1
	exactLimits.MaxTemporaryBytes = 384
	if _, err := verkletree.NewWitness(
		context.Background(), proof, updates, postRoot, exactLimits,
	); err != nil {
		t.Fatalf("exact witness limits error = %v", err)
	}
	precedenceLimits := exactLimits
	precedenceLimits.MaxTemporaryBytes = 1
	if _, err := verkletree.NewWitness(
		context.Background(), proof, []verkletree.Update{verkletree.Delete(key)},
		postRoot, precedenceLimits,
	); resourceOf(err) != verkletree.ResourceTemporaryBytes {
		t.Fatalf("witness temporary preflight error = %v", err)
	}
	if _, err := verkletree.NewWitness(
		context.Background(), proof,
		[]verkletree.Update{verkletree.Delete(key)}, postRoot,
		publicWitnessLimits(),
	); err != nil {
		t.Fatalf("delete witness construction error = %v", err)
	}
	if _, err := verkletree.NewWitness(
		context.Background(), proof,
		[]verkletree.Update{
			verkletree.Set(key, publicValue(2)),
			verkletree.Set(key, publicValue(3)),
		},
		postRoot,
		publicWitnessLimits(),
	); !errors.Is(err, verkletree.ErrDuplicateKey) {
		t.Fatalf("duplicate witness update error = %v", err)
	}
	limits := publicWitnessLimits()
	limits.MaxUpdates = 1
	if _, err := verkletree.NewWitness(
		context.Background(), proof,
		[]verkletree.Update{
			verkletree.Set(key, publicValue(2)),
			verkletree.Set(publicKey(0x30, 0x41), publicValue(3)),
		},
		postRoot,
		limits,
	); !errors.Is(err, verkletree.ErrResourceExhausted) {
		t.Fatalf("witness update budget error = %v", err)
	}

	for name, mutate := range map[string]func([]byte) []byte{
		"magic":    func(value []byte) []byte { value[0] ^= 1; return value },
		"profile":  func(value []byte) []byte { value[4]++; return value },
		"trailing": func(value []byte) []byte { return append(value, 0) },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := mutate(append([]byte(nil), encoded...))
			_, decodeErr := verkletree.DecodeWitness(
				context.Background(), candidate, publicWitnessDecodingLimits(),
			)
			if name == "profile" {
				if !errors.Is(decodeErr, verkletree.ErrUnsupportedProfile) {
					t.Fatalf("profile decode error = %v", decodeErr)
				}
				return
			}
			if !errors.Is(decodeErr, verkletree.ErrInvalidWitness) {
				t.Fatalf("malformed decode error = %v", decodeErr)
			}
		})
	}
	if _, err := verkletree.DecodeWitness(
		nilContext, encoded, publicWitnessDecodingLimits(),
	); !errors.Is(err, verkletree.ErrInvalidContext) {
		t.Fatalf("nil decode context error = %v", err)
	}
	if _, err := verkletree.DecodeWitness(
		context.Background(), encoded, verkletree.WitnessDecodingLimits{},
	); !errors.Is(err, verkletree.ErrInvalidLimits) {
		t.Fatalf("invalid decoding limits error = %v", err)
	}
	decodeLimits := publicWitnessDecodingLimits()
	decodeLimits.MaxPostRootPointDecodes = 0
	if _, err := verkletree.DecodeWitness(
		context.Background(), encoded, decodeLimits,
	); !errors.Is(err, verkletree.ErrInvalidLimits) {
		t.Fatalf("post-root point limit error = %v", err)
	}

	engine, err := verkletree.NewStatelessEngine(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		publicOpeningLimits(),
		publicSnapshotLimits().Commitment,
	)
	if err != nil {
		t.Fatalf("new stateless engine: %v", err)
	}
	if _, err := verkletree.NewStatelessEngine(
		nilContext, verkletree.ExperimentalBandersnatchIPA256V0(),
		publicOpeningLimits(), publicSnapshotLimits().Commitment,
	); !errors.Is(err, verkletree.ErrInvalidContext) {
		t.Fatalf("nil engine context error = %v", err)
	}
	if _, err := verkletree.NewStatelessEngine(
		context.Background(), verkletree.Profile{},
		publicOpeningLimits(), publicSnapshotLimits().Commitment,
	); !errors.Is(err, verkletree.ErrUnsupportedProfile) {
		t.Fatalf("unsupported engine profile error = %v", err)
	}
	if _, err := verkletree.NewStatelessEngine(
		context.Background(), verkletree.ExperimentalBandersnatchIPA256V0(),
		verkletree.OpeningLimits{}, publicSnapshotLimits().Commitment,
	); !errors.Is(err, verkletree.ErrInvalidLimits) {
		t.Fatalf("invalid engine opening limits error = %v", err)
	}
	if _, err := verkletree.NewStatelessEngine(
		context.Background(), verkletree.ExperimentalBandersnatchIPA256V0(),
		publicOpeningLimits(), verkletree.CommitmentLimits{},
	); !errors.Is(err, verkletree.ErrInvalidLimits) {
		t.Fatalf("invalid engine commitment limits error = %v", err)
	}
	wrongSnapshot, err := verkletree.NewSnapshot(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		[]verkletree.Entry{{Key: key, Value: publicValue(9)}},
		publicSnapshotLimits(),
	)
	if err != nil {
		t.Fatalf("different valid snapshot: %v", err)
	}
	wrongRoot, _ := wrongSnapshot.Root()
	wrongWitness, err := verkletree.NewWitness(
		context.Background(), proof, updates, wrongRoot, publicWitnessLimits(),
	)
	if err != nil {
		t.Fatalf("new mismatched witness: %v", err)
	}
	if _, err := engine.Apply(
		context.Background(), wrongWitness,
		publicProofVerificationLimits(), publicStatelessUpdateLimits(),
	); !errors.Is(err, verkletree.ErrPostStateMismatch) {
		t.Fatalf("post-state mismatch error = %v", err)
	}
	missingKey := key
	missingKey[31]++
	if _, err := verkletree.NewWitness(
		context.Background(), proof,
		[]verkletree.Update{verkletree.Set(missingKey, publicValue(4))},
		postRoot, publicWitnessLimits(),
	); !errors.Is(err, verkletree.ErrInvalidWitness) {
		t.Fatalf("mismatched witness claim error = %v", err)
	}
	if _, err := engine.Apply(
		nilContext, witness, publicProofVerificationLimits(), publicStatelessUpdateLimits(),
	); !errors.Is(err, verkletree.ErrInvalidContext) {
		t.Fatalf("nil apply context error = %v", err)
	}
	if _, err := engine.Apply(
		context.Background(), witness,
		verkletree.ProofVerificationLimits{}, publicStatelessUpdateLimits(),
	); !errors.Is(err, verkletree.ErrInvalidLimits) {
		t.Fatalf("invalid verification limits error = %v", err)
	}
	if _, err := engine.Apply(
		context.Background(), witness,
		publicProofVerificationLimits(), verkletree.StatelessUpdateLimits{},
	); !errors.Is(err, verkletree.ErrInvalidLimits) {
		t.Fatalf("invalid stateless limits error = %v", err)
	}

	var zeroWitness verkletree.Witness
	if _, err := zeroWitness.Bytes(
		context.Background(), publicWitnessEncodingLimits(),
	); !errors.Is(err, verkletree.ErrInvalidWitness) {
		t.Fatalf("zero witness bytes error = %v", err)
	}
	if _, err := zeroWitness.Proof(); !errors.Is(err, verkletree.ErrInvalidWitness) {
		t.Fatalf("zero witness proof error = %v", err)
	}
	if _, err := zeroWitness.Updates(context.Background()); !errors.Is(err, verkletree.ErrInvalidWitness) {
		t.Fatalf("zero witness updates error = %v", err)
	}
	if _, err := zeroWitness.PostRoot(); !errors.Is(err, verkletree.ErrInvalidWitness) {
		t.Fatalf("zero witness root error = %v", err)
	}
	if _, err := witness.Bytes(
		nilContext, publicWitnessEncodingLimits(),
	); !errors.Is(err, verkletree.ErrInvalidContext) {
		t.Fatalf("nil witness encoding context error = %v", err)
	}
	if _, err := witness.Bytes(
		context.Background(), verkletree.WitnessEncodingLimits{},
	); !errors.Is(err, verkletree.ErrInvalidLimits) {
		t.Fatalf("invalid witness encoding limits error = %v", err)
	}
	excessiveEncodingLimits := publicWitnessEncodingLimits()
	excessiveEncodingLimits.MaxWitnessBytes = ^uint64(0)
	if _, err := witness.Bytes(
		context.Background(), excessiveEncodingLimits,
	); !errors.Is(err, verkletree.ErrInvalidLimits) {
		t.Fatalf("excessive witness encoding limits error = %v", err)
	}
	excessiveDecodingLimits := publicWitnessDecodingLimits()
	excessiveDecodingLimits.MaxWitnessBytes = ^uint64(0)
	if _, err := verkletree.DecodeWitness(
		context.Background(), encoded, excessiveDecodingLimits,
	); !errors.Is(err, verkletree.ErrInvalidLimits) {
		t.Fatalf("excessive witness decoding limits error = %v", err)
	}
	if _, err := witness.Updates(nilContext); !errors.Is(err, verkletree.ErrInvalidContext) {
		t.Fatalf("nil witness update context error = %v", err)
	}
	if _, err := engine.Apply(
		context.Background(), zeroWitness,
		publicProofVerificationLimits(), publicStatelessUpdateLimits(),
	); !errors.Is(err, verkletree.ErrInvalidWitness) {
		t.Fatalf("zero witness apply error = %v", err)
	}
	var zeroUpdate verkletree.Update
	if _, err := zeroUpdate.Kind(); !errors.Is(err, verkletree.ErrInvalidUpdate) {
		t.Fatalf("zero update kind error = %v", err)
	}
	if _, err := zeroUpdate.Key(); !errors.Is(err, verkletree.ErrInvalidUpdate) {
		t.Fatalf("zero update key error = %v", err)
	}
	if _, _, err := zeroUpdate.Value(); !errors.Is(err, verkletree.ErrInvalidUpdate) {
		t.Fatalf("zero update value error = %v", err)
	}
	if _, present, err := verkletree.Delete(key).Value(); err != nil || present {
		t.Fatalf("delete value = present %v, error %v", present, err)
	}
	var zeroEngine verkletree.StatelessEngine
	if _, err := zeroEngine.Apply(
		context.Background(), witness,
		publicProofVerificationLimits(), publicStatelessUpdateLimits(),
	); !errors.Is(err, verkletree.ErrInvalidStatelessEngine) {
		t.Fatalf("zero stateless engine error = %v", err)
	}
	var zeroResult verkletree.StatelessResult
	if _, err := zeroResult.PreRoot(); !errors.Is(err, verkletree.ErrInvalidStatelessResult) {
		t.Fatalf("zero stateless result pre-root error = %v", err)
	}
	if _, err := zeroResult.PostRoot(); !errors.Is(err, verkletree.ErrInvalidStatelessResult) {
		t.Fatalf("zero stateless result post-root error = %v", err)
	}
}

func publicWitnessLimits() verkletree.WitnessLimits {
	return verkletree.WitnessLimits{MaxUpdates: 64, MaxTemporaryBytes: 1 << 20}
}

func publicWitnessEncodingLimits() verkletree.WitnessEncodingLimits {
	return verkletree.WitnessEncodingLimits{
		MaxWitnessBytes:   128 << 10,
		MaxProofBytes:     64 << 10,
		MaxTemporaryBytes: 256 << 10,
	}
}

func publicWitnessDecodingLimits() verkletree.WitnessDecodingLimits {
	return verkletree.WitnessDecodingLimits{
		MaxWitnessBytes:         128 << 10,
		MaxUpdates:              64,
		MaxPostRootPointDecodes: 1,
		MaxTemporaryBytes:       1 << 20,
		Proof:                   publicProofDecodingLimits(),
	}
}

func publicStatelessUpdateLimits() verkletree.StatelessUpdateLimits {
	return verkletree.StatelessUpdateLimits{
		MaxUpdates:           64,
		MaxCommitmentUpdates: 2_048,
		MaxFieldMappings:     4_096,
		MaxPathLookups:       4_096,
		MaxTemporaryBytes:    8 << 20,
	}
}

func publicTopologyProofGenerationLimits() verkletree.ProofGenerationLimits {
	limits := publicProofGenerationLimits()
	limits.Material.MaxKeys = 1_024
	limits.Material.MaxStemPaths = 1_024
	limits.Material.MaxNodeReads = 32_768
	limits.Material.MaxPathCommitments = 32_768
	limits.Material.MaxPathBytes = 1 << 20
	limits.Material.MaxTemporaryBytes = 64 << 20
	limits.ProverQueries.MaxKeys = 1_024
	limits.ProverQueries.MaxQueries = 4_096
	limits.ProverQueries.MaxNodeReads = 32_768
	limits.ProverQueries.MaxTemporaryBytes = 128 << 20
	limits.VerifierQueries.MaxQueries = 4_096
	limits.VerifierQueries.MaxTemporaryBytes = 64 << 20
	limits.Proof.MaxClaims = 1_024
	limits.Proof.MaxStemPaths = 1_024
	limits.Proof.MaxPathCommitments = 32_768
	limits.Proof.MaxPathDerivations = 32_768
	limits.Proof.MaxPathBytes = 1 << 20
	limits.Proof.MaxTemporaryBytes = 64 << 20

	return limits
}

func publicTopologyProofVerificationLimits() verkletree.ProofVerificationLimits {
	limits := publicProofVerificationLimits()
	limits.VerifierQueries.MaxQueries = 4_096
	limits.VerifierQueries.MaxTemporaryBytes = 64 << 20

	return limits
}

func publicTopologyStatelessUpdateLimits() verkletree.StatelessUpdateLimits {
	limits := publicStatelessUpdateLimits()
	limits.MaxCommitmentUpdates = 4_096
	limits.MaxFieldMappings = 16_384
	limits.MaxPathLookups = 65_536
	limits.MaxTemporaryBytes = 64 << 20

	return limits
}

func assertPublicRootsEqual(t testing.TB, got verkletree.Root, want verkletree.Root) {
	t.Helper()

	gotBytes, err := got.Bytes()
	if err != nil {
		t.Fatalf("root bytes: %v", err)
	}
	wantBytes, err := want.Bytes()
	if err != nil {
		t.Fatalf("expected root bytes: %v", err)
	}
	if gotBytes != wantBytes {
		t.Fatalf("root = %x, want %x", gotBytes, wantBytes)
	}
}

func resourceOf(err error) verkletree.Resource {
	var resourceErr *verkletree.ResourceError
	if errors.As(err, &resourceErr) {
		return resourceErr.Resource
	}

	return 0
}
