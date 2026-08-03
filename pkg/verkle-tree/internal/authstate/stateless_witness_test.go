package authstate

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
	internalprofile "github.com/faustbrian/golib/pkg/verkle-tree/internal/profile"
)

func TestStatelessWitnessCanonicalRoundTrip(t *testing.T) {
	t.Parallel()

	key := testKey(0x12, 0x34)
	snapshot := newTestSnapshot(t, []Entry{{Key: key, Value: testValue(0x56)}})
	proof, updater := newStatelessTestProof(t, snapshot, []Key{key})
	updates := []Update{Set(key, testValue(0x78))}
	postRoot, err := updater.Apply(
		context.Background(),
		proof,
		updates,
		testProofVerificationLimits(),
		testStatelessUpdateLimits(),
	)
	if err != nil {
		t.Fatalf("derive post-state root: %v", err)
	}
	witness, err := NewStatelessWitness(
		context.Background(),
		proof,
		updates,
		postRoot,
		testStatelessWitnessLimits(),
	)
	if err != nil {
		t.Fatalf("construct witness: %v", err)
	}
	encoded, err := witness.Bytes(
		context.Background(),
		testStatelessWitnessEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("encode witness: %v", err)
	}
	decoded, err := DecodeStatelessWitness(
		context.Background(),
		encoded,
		testStatelessWitnessDecodingLimits(),
	)
	if err != nil {
		t.Fatalf("decode witness: %v", err)
	}
	reencoded, err := decoded.Bytes(
		context.Background(),
		testStatelessWitnessEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("re-encode witness: %v", err)
	}
	if string(reencoded) != string(encoded) {
		t.Fatal("canonical witness bytes changed after round trip")
	}

	decodedProof, err := decoded.Proof()
	if err != nil {
		t.Fatalf("decoded proof: %v", err)
	}
	decodedProofBytes, err := decodedProof.Bytes(
		context.Background(),
		testTreeProofEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("encode decoded proof: %v", err)
	}
	proofBytes, err := proof.Bytes(
		context.Background(),
		testTreeProofEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("encode original proof: %v", err)
	}
	if string(decodedProofBytes) != string(proofBytes) {
		t.Fatal("decoded witness proof differs")
	}
	decodedUpdates, err := decoded.Updates(context.Background())
	if err != nil {
		t.Fatalf("decoded updates: %v", err)
	}
	if len(decodedUpdates) != 1 || decodedUpdates[0] != updates[0] {
		t.Fatalf("decoded updates = %v, want %v", decodedUpdates, updates)
	}
	decodedPostRoot, err := decoded.PostRoot()
	if err != nil {
		t.Fatalf("decoded post-state root: %v", err)
	}
	assertSameBackendRoot(t, decodedPostRoot, postRoot)
}

func TestStatelessWitnessCanonicalDeleteRoundTrip(t *testing.T) {
	t.Parallel()

	deleted := testKey(0x18, 0x01)
	retained := testKey(0x18, 0x00)
	snapshot := newTestSnapshot(t, []Entry{
		{Key: deleted, Value: testValue(0x11)},
		{Key: retained, Value: testValue(0x22)},
	})
	proof, updater := newStatelessTestProof(t, snapshot, []Key{deleted, retained})
	updates := []Update{Delete(deleted)}
	postRoot, err := updater.Apply(
		context.Background(), proof, updates,
		testProofVerificationLimits(), testStatelessUpdateLimits(),
	)
	if err != nil {
		t.Fatalf("derive deletion root: %v", err)
	}
	witness, err := NewStatelessWitness(
		context.Background(), proof, updates, postRoot,
		testStatelessWitnessLimits(),
	)
	if err != nil {
		t.Fatalf("construct deletion witness: %v", err)
	}
	encoded, err := witness.Bytes(
		context.Background(), testStatelessWitnessEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("encode deletion witness: %v", err)
	}
	decoded, err := DecodeStatelessWitness(
		context.Background(), encoded, testStatelessWitnessDecodingLimits(),
	)
	if err != nil {
		t.Fatalf("decode deletion witness: %v", err)
	}
	gotUpdates, err := decoded.Updates(context.Background())
	if err != nil || len(gotUpdates) != 1 || gotUpdates[0] != updates[0] {
		t.Fatalf("decoded deletion updates = (%v, %v)", gotUpdates, err)
	}
	gotRoot, err := updater.ApplyWitness(
		context.Background(), decoded,
		testProofVerificationLimits(), testStatelessUpdateLimits(),
	)
	if err != nil {
		t.Fatalf("apply deletion witness: %v", err)
	}
	assertSameBackendRoot(t, gotRoot, postRoot)
}

func TestStatelessWitnessAcceptsCompleteTopologyDeletionProof(t *testing.T) {
	t.Parallel()

	deleted := testKey(0x19, 0x01)
	snapshot := newTestSnapshot(t, []Entry{
		{Key: deleted, Value: testValue(0x11)},
	})
	proof, updater := newTopologyStatelessTestProof(
		t,
		snapshot,
		topologyDisclosureTestKeys(Stem(deleted[:31]), 1),
	)
	updates := []Update{Delete(deleted)}
	postRoot, err := updater.Apply(
		context.Background(), proof, updates,
		topologyProofVerificationLimits(), topologyStatelessUpdateLimits(),
	)
	if err != nil {
		t.Fatalf("derive topology deletion root: %v", err)
	}
	witness, err := NewStatelessWitness(
		context.Background(), proof, updates, postRoot,
		testStatelessWitnessLimits(),
	)
	if err != nil {
		t.Fatalf("construct topology deletion witness: %v", err)
	}
	encodingLimits := StatelessWitnessEncodingLimits{
		MaxWitnessBytes: 8 << 20, MaxProofBytes: 8 << 20,
		MaxTemporaryBytes: 16 << 20,
	}
	encoded, err := witness.Bytes(context.Background(), encodingLimits)
	if err != nil {
		t.Fatalf("encode topology deletion witness: %v", err)
	}
	decodingLimits := testStatelessWitnessDecodingLimits()
	decodingLimits.MaxWitnessBytes = 8 << 20
	decodingLimits.MaxTemporaryBytes = 64 << 20
	decodingLimits.Proof.MaxProofBytes = 8 << 20
	decodingLimits.Proof.MaxClaims = 1_024
	decodingLimits.Proof.MaxStemPaths = 1_024
	decodingLimits.Proof.MaxPathCommitments = 32_768
	decodingLimits.Proof.MaxPathDerivations = 32_768
	decodingLimits.Proof.MaxPathBytes = 1 << 20
	decodingLimits.Proof.MaxPointDecodes = 32_768
	decodingLimits.Proof.MaxScalarDecodes = 1
	decodingLimits.Proof.MaxTemporaryBytes = 64 << 20
	decoded, err := DecodeStatelessWitness(
		context.Background(), encoded, decodingLimits,
	)
	if err != nil {
		t.Fatalf("decode topology deletion witness: %v", err)
	}
	gotRoot, err := updater.ApplyWitness(
		context.Background(), decoded,
		topologyProofVerificationLimits(), topologyStatelessUpdateLimits(),
	)
	if err != nil {
		t.Fatalf("apply topology deletion witness: %v", err)
	}
	assertSameBackendRoot(t, gotRoot, postRoot)
}

func TestStatelessWitnessTopologyClaimFailureBoundaries(t *testing.T) {
	t.Parallel()

	deleted := testKey(0x1a, 0x01)
	surviving := deleted
	surviving[1]++
	snapshot := newTestSnapshot(t, []Entry{
		{Key: deleted, Value: testValue(1)},
		{Key: surviving, Value: testValue(2)},
	})
	stem := Stem(deleted[:31])
	proof, _ := newTopologyStatelessTestProof(
		t, snapshot, topologyDisclosureTestKeys(stem, 2),
	)
	updates := []Update{Delete(deleted)}
	withoutClaim := func(key Key) ClaimSet {
		value := proof.claims
		value.claims = append([]Claim(nil), proof.claims.claims...)
		for index := range value.claims {
			if value.claims[index].key == key {
				value.claims = append(value.claims[:index], value.claims[index+1:]...)

				break
			}
		}

		return value
	}

	if err := validateStatelessWitnessClaims(
		context.Background(), proof.claims, nil, updates,
	); !errors.Is(err, errInvalidStatelessWitness) {
		t.Fatalf("missing topology stem path error = %v", err)
	}
	wrongPaths := append([]StemPath(nil), proof.stemPaths...)
	for index := range wrongPaths {
		if wrongPaths[index].stem == stem {
			wrongPaths[index].kind = StemPathMissing

			break
		}
	}
	if err := validateStatelessWitnessClaims(
		context.Background(), proof.claims, wrongPaths, updates,
	); !errors.Is(err, errInvalidStatelessWitness) {
		t.Fatalf("wrong topology stem path error = %v", err)
	}

	missingSuffix := deleted
	missingSuffix[31] = 0xff
	if err := validateStatelessWitnessClaims(
		context.Background(), withoutClaim(missingSuffix), proof.stemPaths, updates,
	); !errors.Is(err, errInvalidStatelessWitness) {
		t.Fatalf("incomplete suffix disclosure error = %v", err)
	}
	sparseClaims, err := NewClaimSet(
		context.Background(),
		internalprofile.ExperimentalBandersnatchIPA256V0(),
		[]Claim{
			Membership(deleted, testValue(1)),
			Absence(testKey(0x1a, 0x02)),
		},
		testClaimLimits(),
	)
	if err != nil {
		t.Fatalf("construct sparse topology claims: %v", err)
	}
	if err := validateStatelessWitnessClaims(
		context.Background(), sparseClaims, proof.stemPaths, updates,
	); !errors.Is(err, errInvalidStatelessWitness) {
		t.Fatalf("bounded suffix disclosure error = %v", err)
	}
	parent := makeStatelessPath(stem[:1])
	missingParent := statelessTopologyProbe(parent, 0xff)
	if err := validateStatelessWitnessClaims(
		context.Background(), withoutClaim(missingParent), proof.stemPaths, updates,
	); !errors.Is(err, errInvalidStatelessWitness) {
		t.Fatalf("incomplete parent disclosure error = %v", err)
	}

	for successfulChecks, label := range map[int]string{
		1:   "deletion stem",
		2:   "auxiliary scan",
		258: "suffix disclosure",
		515: "parent disclosure",
		771: "final relation",
	} {
		err := validateStatelessWitnessClaims(
			&stepContext{successfulChecks: successfulChecks},
			proof.claims,
			proof.stemPaths,
			updates,
		)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("%s cancellation after %d checks = %v", label, successfulChecks, err)
		}
	}
	assertStatelessWitnessCancellationSweep(t, func(ctx context.Context) error {
		return validateStatelessWitnessClaims(
			ctx, proof.claims, proof.stemPaths, updates,
		)
	})

	other := testKey(0x1b, 0x01)
	retained, disclosed, err := statelessWitnessStemAuxiliaries(
		context.Background(),
		[]Claim{Membership(deleted, testValue(1)), Absence(other)},
		map[Key]struct{}{},
		Stem(deleted[:31]),
	)
	if err != nil || len(retained) != 1 || disclosed {
		t.Fatalf("bounded auxiliary scan = retained %d, disclosed %v, error %v", len(retained), disclosed, err)
	}
	if _, _, err := statelessWitnessStemAuxiliaries(
		&stepContext{}, proof.claims.claims, map[Key]struct{}{}, stem,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("auxiliary cancellation error = %v", err)
	}
	if _, found := statelessWitnessStemPath(proof.stemPaths, Stem(other[:31])); found {
		t.Fatal("missing witness stem path was found")
	}
}

func TestStatelessWitnessDecoderRejectsShortEmbeddedProof(t *testing.T) {
	t.Parallel()

	key := testKey(0x1c, 0x01)
	snapshot := newTestSnapshot(t, []Entry{{Key: key, Value: testValue(1)}})
	proof, updater := newStatelessTestProof(t, snapshot, []Key{key})
	updates := []Update{Set(key, testValue(2))}
	postRoot, err := updater.Apply(
		context.Background(), proof, updates,
		testProofVerificationLimits(), testStatelessUpdateLimits(),
	)
	if err != nil {
		t.Fatalf("derive post-state root: %v", err)
	}
	witness, err := NewStatelessWitness(
		context.Background(), proof, updates, postRoot, testStatelessWitnessLimits(),
	)
	if err != nil {
		t.Fatalf("construct witness: %v", err)
	}
	encoded, err := witness.Bytes(
		context.Background(), testStatelessWitnessEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("encode witness: %v", err)
	}
	proofSize := treeProofHeaderBytes - 1
	short := append(
		[]byte(nil),
		encoded[:statelessWitnessHeaderBytes+statelessWitnessUpdateBytes+proofSize]...,
	)
	binary.BigEndian.PutUint32(short[9:13], uint32(proofSize))
	if _, err := DecodeStatelessWitness(
		context.Background(), short, testStatelessWitnessDecodingLimits(),
	); !errors.Is(err, errInvalidStatelessWitnessEncoding) {
		t.Fatalf("short embedded proof error = %v", err)
	}
}

func TestStatelessUpdaterVerifiesWitnessPostStateRoot(t *testing.T) {
	t.Parallel()

	key := testKey(0x21, 0x43)
	snapshot := newTestSnapshot(t, []Entry{{Key: key, Value: testValue(0x65)}})
	proof, updater := newStatelessTestProof(t, snapshot, []Key{key})
	updates := []Update{Set(key, testValue(0x87))}
	wantSnapshot, _, err := snapshot.Apply(context.Background(), updates)
	if err != nil {
		t.Fatalf("apply stateful updates: %v", err)
	}
	wantRoot, err := wantSnapshot.RootContainer(context.Background())
	if err != nil {
		t.Fatalf("stateful post-state root: %v", err)
	}
	witness, err := NewStatelessWitness(
		context.Background(),
		proof,
		updates,
		wantRoot,
		testStatelessWitnessLimits(),
	)
	if err != nil {
		t.Fatalf("construct witness: %v", err)
	}
	got, err := updater.ApplyWitness(
		context.Background(),
		witness,
		testProofVerificationLimits(),
		testStatelessUpdateLimits(),
	)
	if err != nil {
		t.Fatalf("apply witness: %v", err)
	}
	assertSameBackendRoot(t, got, wantRoot)

	wrongSnapshot := newTestSnapshot(t, []Entry{{Key: key, Value: testValue(0xa9)}})
	wrongRoot, err := wrongSnapshot.RootContainer(context.Background())
	if err != nil {
		t.Fatalf("different valid root: %v", err)
	}
	wrong, err := NewStatelessWitness(
		context.Background(),
		proof,
		updates,
		wrongRoot,
		testStatelessWitnessLimits(),
	)
	if err != nil {
		t.Fatalf("construct mismatched witness: %v", err)
	}
	if _, err := updater.ApplyWitness(
		context.Background(),
		wrong,
		testProofVerificationLimits(),
		testStatelessUpdateLimits(),
	); !errors.Is(err, errStatelessPostRootMismatch) {
		t.Fatalf("mismatched post-state root error = %v", err)
	}
}

func TestStatelessWitnessRejectsInvalidConstructionAndAccess(t *testing.T) {
	t.Parallel()

	key := testKey(0x31, 0x41)
	snapshot := newTestSnapshot(t, []Entry{{Key: key, Value: testValue(1)}})
	proof, updater := newStatelessTestProof(t, snapshot, []Key{key})
	updates := []Update{Set(key, testValue(2))}
	postRoot, err := updater.Apply(
		context.Background(), proof, updates,
		testProofVerificationLimits(), testStatelessUpdateLimits(),
	)
	if err != nil {
		t.Fatalf("derive post-state root: %v", err)
	}
	construct := func(candidateProof TreeProof, candidate []Update, root backend.Root, limits StatelessWitnessLimits) error {
		_, constructErr := NewStatelessWitness(
			context.Background(), candidateProof, candidate, root, limits,
		)
		return constructErr
	}
	for name, invalidate := range map[string]func(*StatelessWitnessLimits){
		"updates zero":    func(value *StatelessWitnessLimits) { value.MaxUpdates = 0 },
		"updates maximum": func(value *StatelessWitnessLimits) { value.MaxUpdates = maxStatelessUpdates + 1 },
		"temporary zero":  func(value *StatelessWitnessLimits) { value.MaxTemporaryBytes = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			limits := testStatelessWitnessLimits()
			invalidate(&limits)
			if err := construct(proof, updates, postRoot, limits); !errors.Is(err, errInvalidStatelessWitnessLimits) {
				t.Fatalf("invalid limits error = %v", err)
			}
		})
	}
	if err := construct(TreeProof{}, updates, postRoot, testStatelessWitnessLimits()); !errors.Is(err, errInvalidStatelessWitness) {
		t.Fatalf("invalid proof error = %v", err)
	}
	if err := construct(proof, updates, backend.Root{}, testStatelessWitnessLimits()); !errors.Is(err, errInvalidStatelessWitness) {
		t.Fatalf("invalid post-root error = %v", err)
	}
	if err := construct(proof, nil, postRoot, testStatelessWitnessLimits()); !errors.Is(err, errInvalidStatelessWitness) {
		t.Fatalf("empty update error = %v", err)
	}
	surplusKey := testKey(0x31, 0x42)
	surplusProof, _ := newStatelessTestProof(t, snapshot, []Key{key, surplusKey})
	if err := construct(
		surplusProof, updates, postRoot, testStatelessWitnessLimits(),
	); !errors.Is(err, errInvalidStatelessWitness) {
		t.Fatalf("surplus proof claim error = %v", err)
	}
	secondRetained := testKey(0x31, 0x43)
	retainedSnapshot := newTestSnapshot(t, []Entry{
		{Key: key, Value: testValue(1)},
		{Key: surplusKey, Value: testValue(2)},
		{Key: secondRetained, Value: testValue(3)},
	})
	duplicateRetainedProof, _ := newStatelessTestProof(
		t, retainedSnapshot, []Key{key, surplusKey, secondRetained},
	)
	if err := construct(
		duplicateRetainedProof, []Update{Delete(key)}, postRoot,
		testStatelessWitnessLimits(),
	); !errors.Is(err, errInvalidStatelessWitness) {
		t.Fatalf("duplicate retained proof claim error = %v", err)
	}
	setKey := testKey(0x31, 0x44)
	redundantRetainedProof, _ := newStatelessTestProof(
		t, retainedSnapshot, []Key{key, surplusKey, setKey},
	)
	if err := construct(
		redundantRetainedProof,
		[]Update{Delete(key), Set(setKey, testValue(4))}, postRoot,
		testStatelessWitnessLimits(),
	); !errors.Is(err, errInvalidStatelessWitness) {
		t.Fatalf("redundant retained proof claim error = %v", err)
	}
	absentDelete := testKey(0x31, 0x45)
	absentDeleteProof, _ := newStatelessTestProof(
		t, retainedSnapshot, []Key{surplusKey, absentDelete},
	)
	if err := construct(
		absentDeleteProof, []Update{Delete(absentDelete)}, postRoot,
		testStatelessWitnessLimits(),
	); !errors.Is(err, errInvalidStatelessWitness) {
		t.Fatalf("absent-delete retained proof claim error = %v", err)
	}
	if err := construct(
		proof, []Update{Set(surplusKey, testValue(2))}, postRoot,
		testStatelessWitnessLimits(),
	); !errors.Is(err, errInvalidStatelessWitness) {
		t.Fatalf("mismatched proof claim error = %v", err)
	}
	if err := construct(
		proof, []Update{Delete(surplusKey)}, postRoot,
		testStatelessWitnessLimits(),
	); !errors.Is(err, errInvalidStatelessWitness) {
		t.Fatalf("mismatched delete claim error = %v", err)
	}
	if err := validateStatelessWitnessClaims(
		context.Background(), ClaimSet{}, nil, []Update{Delete(key)},
	); !errors.Is(err, errInvalidClaimSet) {
		t.Fatalf("invalid claim set relation error = %v", err)
	}
	if err := construct(proof, []Update{{}}, postRoot, testStatelessWitnessLimits()); !errors.Is(err, errInvalidStatelessWitness) {
		t.Fatalf("invalid update error = %v", err)
	}
	if err := construct(proof, []Update{Delete(key)}, postRoot, testStatelessWitnessLimits()); err != nil {
		t.Fatalf("delete update construction error = %v", err)
	}
	if err := construct(proof, []Update{updates[0], updates[0]}, postRoot, testStatelessWitnessLimits()); !errors.Is(err, errDuplicateKey) {
		t.Fatalf("duplicate update error = %v", err)
	}
	limits := testStatelessWitnessLimits()
	limits.MaxUpdates = 1
	if err := construct(proof, append(updates, Set(testKey(0x31, 0x42), testValue(3))), postRoot, limits); !errors.Is(err, errStatelessWitnessResource) {
		t.Fatalf("update resource error = %v", err)
	}
	limits = testStatelessWitnessLimits()
	limits.MaxTemporaryBytes = statelessWitnessUpdateScratch*2 - 1
	if err := construct(proof, updates, postRoot, limits); !errors.Is(err, errStatelessWitnessResource) {
		t.Fatalf("temporary resource error = %v", err)
	}

	witness, err := NewStatelessWitness(
		context.Background(), proof, updates, postRoot, testStatelessWitnessLimits(),
	)
	if err != nil {
		t.Fatalf("construct witness: %v", err)
	}
	copied, err := witness.Updates(context.Background())
	if err != nil {
		t.Fatalf("copy updates: %v", err)
	}
	copied[0] = Set(key, testValue(9))
	again, _ := witness.Updates(context.Background())
	if again[0] != updates[0] {
		t.Fatal("returned updates alias witness state")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := witness.Updates(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled update copy error = %v", err)
	}
	corruptCancelled := witness
	corruptCancelled.updates = append([]Update(nil), witness.updates...)
	corruptCancelled.updates[len(corruptCancelled.updates)-1] = Update{}
	if _, err := corruptCancelled.Updates(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled corrupt update copy error = %v", err)
	}

	var zero StatelessWitness
	if _, err := zero.Proof(); !errors.Is(err, errInvalidStatelessWitness) {
		t.Fatalf("zero witness proof error = %v", err)
	}
	if _, err := zero.Updates(context.Background()); !errors.Is(err, errInvalidStatelessWitness) {
		t.Fatalf("zero witness updates error = %v", err)
	}
	if _, err := zero.PostRoot(); !errors.Is(err, errInvalidStatelessWitness) {
		t.Fatalf("zero witness root error = %v", err)
	}
	if _, err := zero.Bytes(
		context.Background(), testStatelessWitnessEncodingLimits(),
	); !errors.Is(err, errInvalidStatelessWitness) {
		t.Fatalf("zero witness bytes error = %v", err)
	}
	if _, err := updater.ApplyWitness(
		context.Background(), zero,
		testProofVerificationLimits(), testStatelessUpdateLimits(),
	); !errors.Is(err, errInvalidStatelessWitness) {
		t.Fatalf("invalid apply witness error = %v", err)
	}
	var zeroUpdater *StatelessUpdater
	if _, err := zeroUpdater.ApplyWitness(
		context.Background(), witness,
		testProofVerificationLimits(), testStatelessUpdateLimits(),
	); !errors.Is(err, errInvalidStatelessUpdater) {
		t.Fatalf("invalid updater error = %v", err)
	}

	for name, corrupt := range map[string]StatelessWitness{
		"profile":       {valid: true},
		"empty updates": {profile: witness.profile, proof: proof, postRoot: postRoot, valid: true},
		"proof":         {profile: witness.profile, postRoot: postRoot, updates: updates, valid: true},
		"post root":     {profile: witness.profile, proof: proof, updates: updates, valid: true},
		"mismatched claim": {
			profile: witness.profile, proof: proof,
			updates: []Update{Set(surplusKey, testValue(2))}, postRoot: postRoot, valid: true,
		},
		"updates": {
			profile: witness.profile, proof: proof,
			updates:  []Update{{kind: UpdateDelete, value: testValue(9)}},
			postRoot: postRoot, valid: true,
		},
		"duplicate updates": {
			profile: witness.profile, proof: proof,
			updates:  []Update{updates[0], updates[0]},
			postRoot: postRoot, valid: true,
		},
	} {
		t.Run("corrupt "+name, func(t *testing.T) {
			if err := corrupt.validate(context.Background()); !errors.Is(err, errInvalidStatelessWitness) {
				t.Fatalf("corrupt witness validation error = %v", err)
			}
		})
	}
}

func TestStatelessWitnessAcceptsExactMaximumLimits(t *testing.T) {
	t.Parallel()

	maximumWitnessBytes := uint64(statelessWitnessHeaderBytes) +
		uint64(maxStatelessUpdates)*uint64(statelessWitnessUpdateBytes) +
		uint64(maxTreeProofEncodedBytes)
	if err := (StatelessWitnessLimits{
		MaxUpdates: maxStatelessUpdates, MaxTemporaryBytes: 1,
	}).validate(); err != nil {
		t.Fatalf("exact construction limits: %v", err)
	}
	if err := (StatelessWitnessEncodingLimits{
		MaxWitnessBytes:   maximumWitnessBytes,
		MaxProofBytes:     uint64(maxTreeProofEncodedBytes),
		MaxTemporaryBytes: 1,
	}).validate(); err != nil {
		t.Fatalf("exact encoding limits: %v", err)
	}
	if err := (StatelessWitnessDecodingLimits{
		MaxWitnessBytes:         maximumWitnessBytes,
		MaxUpdates:              maxStatelessUpdates,
		MaxPostRootPointDecodes: 1,
		MaxTemporaryBytes:       1,
		Proof:                   testTreeProofDecodingLimits(),
	}).validate(); err != nil {
		t.Fatalf("exact decoding limits: %v", err)
	}
}

func TestStatelessWitnessCancellationBoundaries(t *testing.T) {
	t.Parallel()

	first := testKey(0x71, 0x01)
	second := testKey(0x71, 0x02)
	snapshot := newTestSnapshot(t, []Entry{
		{Key: first, Value: testValue(1)},
		{Key: second, Value: testValue(2)},
	})
	proof, updater := newStatelessTestProof(t, snapshot, []Key{first, second})
	updates := []Update{Set(second, testValue(4)), Set(first, testValue(3))}
	postRoot, err := updater.Apply(
		context.Background(), proof, updates,
		testProofVerificationLimits(), testStatelessUpdateLimits(),
	)
	if err != nil {
		t.Fatalf("derive post-state root: %v", err)
	}
	witness, err := NewStatelessWitness(
		context.Background(), proof, updates, postRoot, testStatelessWitnessLimits(),
	)
	if err != nil {
		t.Fatalf("construct witness: %v", err)
	}
	encoded, err := witness.Bytes(
		context.Background(), testStatelessWitnessEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("encode witness: %v", err)
	}

	assertStatelessWitnessCancellationSweep(t, func(ctx context.Context) error {
		_, operationErr := NewStatelessWitness(
			ctx, proof, updates, postRoot, testStatelessWitnessLimits(),
		)
		return operationErr
	})
	assertStatelessWitnessCancellationSweep(t, func(ctx context.Context) error {
		_, operationErr := witness.Updates(ctx)
		return operationErr
	})
	assertStatelessWitnessCancellationSweep(t, func(ctx context.Context) error {
		_, operationErr := witness.Bytes(ctx, testStatelessWitnessEncodingLimits())
		return operationErr
	})
	assertStatelessWitnessCancellationSweep(t, func(ctx context.Context) error {
		_, operationErr := DecodeStatelessWitness(
			ctx, encoded, testStatelessWitnessDecodingLimits(),
		)
		return operationErr
	})

	mismatched := []Update{
		Set(first, testValue(3)),
		Set(testKey(0x71, 0x03), testValue(4)),
	}
	_, err = NewStatelessWitness(
		&stepContext{successfulChecks: 10},
		proof,
		mismatched,
		postRoot,
		testStatelessWitnessLimits(),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("claim-key comparison cancellation error = %v", err)
	}
}

func TestStatelessWitnessEncodingRejectsMalformedAndExhaustiveLimits(t *testing.T) {
	t.Parallel()

	key := testKey(0x51, 0x61)
	snapshot := newTestSnapshot(t, []Entry{{Key: key, Value: testValue(1)}})
	proof, updater := newStatelessTestProof(t, snapshot, []Key{key})
	updates := []Update{Set(key, testValue(2))}
	postRoot, err := updater.Apply(
		context.Background(), proof, updates,
		testProofVerificationLimits(), testStatelessUpdateLimits(),
	)
	if err != nil {
		t.Fatalf("derive post-state root: %v", err)
	}
	witness, err := NewStatelessWitness(
		context.Background(), proof, updates, postRoot, testStatelessWitnessLimits(),
	)
	if err != nil {
		t.Fatalf("construct witness: %v", err)
	}
	encoded, err := witness.Bytes(
		context.Background(), testStatelessWitnessEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("encode witness: %v", err)
	}

	for name, invalidate := range map[string]func(*StatelessWitnessEncodingLimits){
		"witness zero": func(value *StatelessWitnessEncodingLimits) { value.MaxWitnessBytes = 0 },
		"witness maximum": func(value *StatelessWitnessEncodingLimits) {
			value.MaxWitnessBytes = uint64(maxStatelessWitnessEncodedBytes) + 1
		},
		"proof zero": func(value *StatelessWitnessEncodingLimits) { value.MaxProofBytes = 0 },
		"proof maximum": func(value *StatelessWitnessEncodingLimits) {
			value.MaxProofBytes = uint64(maxTreeProofEncodedBytes) + 1
		},
		"temporary zero": func(value *StatelessWitnessEncodingLimits) { value.MaxTemporaryBytes = 0 },
	} {
		t.Run("encoding "+name, func(t *testing.T) {
			limits := testStatelessWitnessEncodingLimits()
			invalidate(&limits)
			if _, err := witness.Bytes(context.Background(), limits); !errors.Is(err, errInvalidStatelessWitnessEncodingLimits) {
				t.Fatalf("invalid encoding limits error = %v", err)
			}
		})
	}
	encodingLimits := testStatelessWitnessEncodingLimits()
	encodingLimits.MaxWitnessBytes = uint64(len(encoded) - 1)
	assertStatelessWitnessResource(t, witnessBytesError(witness, encodingLimits), StatelessWitnessResourceBytes)
	encodingLimits = testStatelessWitnessEncodingLimits()
	encodingLimits.MaxProofBytes = treeProofEncodedSize(proof) - 1
	assertStatelessWitnessResource(t, witnessBytesError(witness, encodingLimits), StatelessWitnessResourceProofBytes)
	encodingLimits = testStatelessWitnessEncodingLimits()
	encodingLimits.MaxTemporaryBytes = uint64(len(encoded)) + treeProofEncodedSize(proof) - 1
	assertStatelessWitnessResource(t, witnessBytesError(witness, encodingLimits), StatelessWitnessResourceTemporaryBytes)

	for name, invalidate := range map[string]func(*StatelessWitnessDecodingLimits){
		"witness zero": func(value *StatelessWitnessDecodingLimits) { value.MaxWitnessBytes = 0 },
		"witness maximum": func(value *StatelessWitnessDecodingLimits) {
			value.MaxWitnessBytes = uint64(maxStatelessWitnessEncodedBytes) + 1
		},
		"updates zero": func(value *StatelessWitnessDecodingLimits) { value.MaxUpdates = 0 },
		"updates maximum": func(value *StatelessWitnessDecodingLimits) {
			value.MaxUpdates = maxStatelessUpdates + 1
		},
		"post-root points zero": func(value *StatelessWitnessDecodingLimits) {
			value.MaxPostRootPointDecodes = 0
		},
		"post-root points maximum": func(value *StatelessWitnessDecodingLimits) {
			value.MaxPostRootPointDecodes = 2
		},
		"temporary zero": func(value *StatelessWitnessDecodingLimits) { value.MaxTemporaryBytes = 0 },
		"proof invalid":  func(value *StatelessWitnessDecodingLimits) { value.Proof = TreeProofDecodingLimits{} },
	} {
		t.Run("decoding "+name, func(t *testing.T) {
			limits := testStatelessWitnessDecodingLimits()
			invalidate(&limits)
			if _, err := DecodeStatelessWitness(context.Background(), encoded, limits); !errors.Is(err, errInvalidStatelessWitnessDecodingLimits) {
				t.Fatalf("invalid decoding limits error = %v", err)
			}
		})
	}
	decodingLimits := testStatelessWitnessDecodingLimits()
	decodingLimits.MaxWitnessBytes = uint64(len(encoded) - 1)
	assertStatelessWitnessResource(t, decodeStatelessWitnessError(encoded, decodingLimits), StatelessWitnessResourceBytes)
	decodingLimits = testStatelessWitnessDecodingLimits()
	decodingLimits.MaxUpdates = 0
	if _, err := DecodeStatelessWitness(context.Background(), encoded, decodingLimits); !errors.Is(err, errInvalidStatelessWitnessDecodingLimits) {
		t.Fatalf("zero update decoding limit error = %v", err)
	}
	decodingLimits = testStatelessWitnessDecodingLimits()
	decodingLimits.MaxTemporaryBytes = uint64(len(encoded)) + 2*statelessWitnessUpdateScratch - 1
	assertStatelessWitnessResource(t, decodeStatelessWitnessError(encoded, decodingLimits), StatelessWitnessResourceTemporaryBytes)
	proofSize := binary.BigEndian.Uint32(encoded[9:13])
	decodingLimits = testStatelessWitnessDecodingLimits()
	decodingLimits.Proof.MaxProofBytes = uint64(proofSize - 1)
	assertStatelessWitnessResource(t, decodeStatelessWitnessError(encoded, decodingLimits), StatelessWitnessResourceProofBytes)

	secondKey := key
	secondKey[31]++
	twoProof, twoUpdater := newStatelessTestProof(t, snapshot, []Key{key, secondKey})
	twoWitnessUpdates := []Update{
		Set(key, testValue(2)), Set(secondKey, testValue(3)),
	}
	twoPostRoot, err := twoUpdater.Apply(
		context.Background(), twoProof, twoWitnessUpdates,
		testProofVerificationLimits(), testStatelessUpdateLimits(),
	)
	if err != nil {
		t.Fatalf("derive two-update post-state root: %v", err)
	}
	twoWitness, err := NewStatelessWitness(
		context.Background(), twoProof, twoWitnessUpdates, twoPostRoot,
		testStatelessWitnessLimits(),
	)
	if err != nil {
		t.Fatalf("construct two-update witness: %v", err)
	}
	twoUpdates, err := twoWitness.Bytes(
		context.Background(), testStatelessWitnessEncodingLimits(),
	)
	if err != nil {
		t.Fatalf("encode two-update witness: %v", err)
	}
	secondUpdateOffset := statelessWitnessHeaderBytes + statelessWitnessUpdateBytes
	duplicateUpdates := append([]byte(nil), twoUpdates...)
	copy(
		duplicateUpdates[secondUpdateOffset:secondUpdateOffset+statelessWitnessUpdateBytes],
		duplicateUpdates[statelessWitnessHeaderBytes:statelessWitnessHeaderBytes+statelessWitnessUpdateBytes],
	)
	decodingLimits = testStatelessWitnessDecodingLimits()
	decodingLimits.MaxUpdates = 1
	assertStatelessWitnessResource(t, decodeStatelessWitnessError(twoUpdates, decodingLimits), StatelessWitnessResourceUpdates)
	if _, err := DecodeStatelessWitness(
		context.Background(), duplicateUpdates, testStatelessWitnessDecodingLimits(),
	); !errors.Is(err, errInvalidStatelessWitnessEncoding) {
		t.Fatalf("duplicate encoded update error = %v", err)
	}
	if _, err := DecodeStatelessWitness(
		context.Background(), twoUpdates, testStatelessWitnessDecodingLimits(),
	); err != nil {
		t.Fatalf("ordered two-update encoding: %v", err)
	}
	corruptProof := append([]byte(nil), encoded...)
	corruptProof[statelessWitnessHeaderBytes+statelessWitnessUpdateBytes] ^= 1
	if _, err := DecodeStatelessWitness(
		context.Background(), corruptProof, testStatelessWitnessDecodingLimits(),
	); err == nil {
		t.Fatal("corrupt embedded proof header was accepted")
	}

	mutations := map[string]func([]byte){
		"magic":         func(value []byte) { value[0] ^= 1 },
		"profile":       func(value []byte) { value[4]++ },
		"version":       func(value []byte) { value[6]++ },
		"encoding":      func(value []byte) { value[8]++ },
		"proof length":  func(value []byte) { binary.BigEndian.PutUint32(value[9:13], 0) },
		"update count":  func(value []byte) { binary.BigEndian.PutUint32(value[13:17], 0) },
		"update kind":   func(value []byte) { value[statelessWitnessHeaderBytes] = 3 },
		"delete value":  func(value []byte) { value[statelessWitnessHeaderBytes] = byte(UpdateDelete) },
		"update key":    func(value []byte) { value[statelessWitnessHeaderBytes+32]++ },
		"post root":     func(value []byte) { value[17] ^= 1 },
		"proof payload": func(value []byte) { value[len(value)-1] ^= 1 },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			candidate := append([]byte(nil), encoded...)
			mutate(candidate)
			_, decodeErr := DecodeStatelessWitness(
				context.Background(), candidate, testStatelessWitnessDecodingLimits(),
			)
			if name == "profile" || name == "version" || name == "encoding" {
				if !errors.Is(decodeErr, internalprofile.ErrUnsupported) {
					t.Fatalf("profile error = %v", decodeErr)
				}
				return
			}
			if name == "proof payload" && decodeErr == nil {
				decoded, decodeErr := DecodeStatelessWitness(
					context.Background(), candidate, testStatelessWitnessDecodingLimits(),
				)
				if decodeErr != nil {
					t.Fatalf("decode canonical mutated proof: %v", decodeErr)
				}
				if _, verifyErr := updater.ApplyWitness(
					context.Background(), decoded,
					testProofVerificationLimits(), testStatelessUpdateLimits(),
				); !IsProofVerificationError(verifyErr) {
					t.Fatalf("mutated proof verification error = %v", verifyErr)
				}
				return
			}
			if decodeErr == nil {
				t.Fatal("malformed witness was accepted")
			}
		})
	}
	for _, candidate := range [][]byte{nil, encoded[:statelessWitnessHeaderBytes-1], append(encoded, 0)} {
		if _, err := DecodeStatelessWitness(
			context.Background(), candidate, testStatelessWitnessDecodingLimits(),
		); err == nil {
			t.Fatal("wrong-length witness was accepted")
		}
	}
}

func TestStatelessWitnessErrorsAndUpdateAccessors(t *testing.T) {
	t.Parallel()

	resourceErr := &StatelessWitnessResourceError{
		Resource: StatelessWitnessResourceBytes, Limit: 1, Actual: 2,
	}
	if resourceErr.Error() == "" || resourceErr.Unwrap() != errStatelessWitnessResource {
		t.Fatal("witness resource error contract mismatch")
	}
	other := errors.New("different")
	if !IsInvalidStatelessWitnessError(errInvalidStatelessWitness) ||
		IsInvalidStatelessWitnessError(other) ||
		!IsInvalidStatelessWitnessEncodingError(errInvalidStatelessWitnessEncoding) ||
		IsInvalidStatelessWitnessEncodingError(other) ||
		!IsInvalidStatelessWitnessLimitsError(errInvalidStatelessWitnessLimits) ||
		!IsInvalidStatelessWitnessLimitsError(errInvalidStatelessWitnessEncodingLimits) ||
		!IsInvalidStatelessWitnessLimitsError(errInvalidStatelessWitnessDecodingLimits) ||
		IsInvalidStatelessWitnessLimitsError(other) ||
		!IsStatelessPostRootMismatchError(errStatelessPostRootMismatch) ||
		IsStatelessPostRootMismatchError(other) ||
		!IsInvalidStatelessUpdaterError(errInvalidStatelessUpdater) ||
		IsInvalidStatelessUpdaterError(other) ||
		!IsInvalidStatelessUpdateError(errInvalidStatelessUpdate) ||
		IsInvalidStatelessUpdateError(other) ||
		!IsIncompleteStatelessWitnessError(errIncompleteStatelessWitness) ||
		IsIncompleteStatelessWitnessError(other) ||
		!IsUnsupportedStatelessUpdateError(errUnsupportedStatelessUpdate) ||
		IsUnsupportedStatelessUpdateError(other) {
		t.Fatal("stateless witness error classifier mismatch")
	}

	key := testKey(1, 2)
	set := Set(key, testValue(3))
	kind, kindErr := set.Kind()
	gotKey, keyErr := set.Key()
	value, present, valueErr := set.Value()
	if kindErr != nil || keyErr != nil || valueErr != nil ||
		kind != UpdateSet || gotKey != key || !present || value != testValue(3) {
		t.Fatal("Set accessors mismatch")
	}
	deleted := Delete(key)
	_, present, valueErr = deleted.Value()
	if valueErr != nil || present {
		t.Fatal("Delete value accessor mismatch")
	}
	forged := Update{kind: UpdateDelete, value: testValue(1)}
	if _, err := forged.Kind(); !errors.Is(err, errInvalidUpdate) {
		t.Fatalf("invalid update kind error = %v", err)
	}
	if _, err := forged.Key(); !errors.Is(err, errInvalidUpdate) {
		t.Fatalf("invalid update key error = %v", err)
	}
	if _, _, err := forged.Value(); !errors.Is(err, errInvalidUpdate) {
		t.Fatalf("invalid update value error = %v", err)
	}
}

func witnessBytesError(
	witness StatelessWitness,
	limits StatelessWitnessEncodingLimits,
) error {
	_, err := witness.Bytes(context.Background(), limits)

	return err
}

func decodeStatelessWitnessError(
	encoded []byte,
	limits StatelessWitnessDecodingLimits,
) error {
	_, err := DecodeStatelessWitness(context.Background(), encoded, limits)

	return err
}

func assertStatelessWitnessResource(
	t testing.TB,
	err error,
	resource StatelessWitnessResource,
) {
	t.Helper()

	var resourceErr *StatelessWitnessResourceError
	if !errors.As(err, &resourceErr) ||
		resourceErr.Resource != resource ||
		!errors.Is(err, errStatelessWitnessResource) {
		t.Fatalf("witness resource error = %v, want resource %d", err, resource)
	}
}

func assertStatelessWitnessCancellationSweep(
	t testing.TB,
	operation func(context.Context) error,
) {
	t.Helper()

	observed := false
	for successful := 0; successful < 2_000; successful++ {
		err := operation(&stepContext{successfulChecks: successful})
		if err == nil {
			if !observed {
				t.Fatal("no cancellation boundary was exercised")
			}
			return
		}
		observed = true
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation after %d checks = %v", successful, err)
		}
	}

	t.Fatal("cancellation sweep did not reach success")
}

func testStatelessWitnessLimits() StatelessWitnessLimits {
	return StatelessWitnessLimits{
		MaxUpdates:        16,
		MaxTemporaryBytes: 1 << 20,
	}
}

func testStatelessWitnessEncodingLimits() StatelessWitnessEncodingLimits {
	return StatelessWitnessEncodingLimits{
		MaxWitnessBytes:   1 << 20,
		MaxProofBytes:     1 << 20,
		MaxTemporaryBytes: 2 << 20,
	}
}

func testStatelessWitnessDecodingLimits() StatelessWitnessDecodingLimits {
	return StatelessWitnessDecodingLimits{
		MaxWitnessBytes:         1 << 20,
		MaxUpdates:              16,
		MaxPostRootPointDecodes: 1,
		MaxTemporaryBytes:       4 << 20,
		Proof:                   testTreeProofDecodingLimits(),
	}
}
