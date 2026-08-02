package backend

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"
)

func TestCommitmentEngineUpdateCommitmentHandlesIdentityAndNoOp(t *testing.T) {
	t.Parallel()

	engine, err := NewCommitmentEngine(context.Background(), testCommitmentLimits())
	if err != nil {
		t.Fatalf("new commitment engine: %v", err)
	}
	identity := EmptyVectorCommitment()
	unchanged, err := engine.UpdateCommitment(context.Background(), identity, nil)
	if err != nil {
		t.Fatalf("empty update: %v", err)
	}
	if empty, classifyErr := unchanged.IsIdentity(); classifyErr != nil || !empty {
		t.Fatalf("empty update identity = %t, error %v", empty, classifyErr)
	}

	var vector Vector
	setVectorUint64(&vector, 42, 9)
	committed, err := engine.Commit(context.Background(), vector)
	if err != nil {
		t.Fatalf("commit vector: %v", err)
	}
	removed, err := engine.UpdateCommitment(
		context.Background(),
		committed,
		[]VectorUpdate{{Index: 42, Old: vector[42]}},
	)
	if err != nil {
		t.Fatalf("remove final scalar: %v", err)
	}
	if empty, classifyErr := removed.IsIdentity(); classifyErr != nil || !empty {
		t.Fatalf("removed commitment identity = %t, error %v", empty, classifyErr)
	}

	unchanged, err = engine.UpdateCommitment(
		context.Background(),
		committed,
		[]VectorUpdate{{Index: 42, Old: vector[42], New: vector[42]}},
	)
	if err != nil {
		t.Fatalf("equal scalar update: %v", err)
	}
	got, _ := unchanged.DeduplicationKey()
	want, _ := committed.DeduplicationKey()
	if got != want {
		t.Fatalf("equal scalar update changed commitment: %x, want %x", got, want)
	}
}

func TestCommitmentEngineUpdateCommitmentAcceptsEveryVectorPosition(t *testing.T) {
	t.Parallel()

	limits := testCommitmentLimits()
	limits.MaxScalarDecodes = 2 * VectorWidth
	engine, err := NewCommitmentEngine(context.Background(), limits)
	if err != nil {
		t.Fatalf("new commitment engine: %v", err)
	}
	after, _ := commitmentFixtureVector(t, "dense-incrementing")
	want, err := engine.Commit(context.Background(), after)
	if err != nil {
		t.Fatalf("commit expected dense vector: %v", err)
	}
	updates := make([]VectorUpdate, VectorWidth)
	for index := range updates {
		updates[index] = VectorUpdate{
			Index: uint8(index),
			New:   after[index],
		}
	}

	got, err := engine.UpdateCommitment(
		context.Background(), EmptyVectorCommitment(), updates,
	)
	if err != nil {
		t.Fatalf("update every vector position: %v", err)
	}
	gotKey, err := got.DeduplicationKey()
	if err != nil {
		t.Fatalf("updated commitment key: %v", err)
	}
	wantKey, err := want.DeduplicationKey()
	if err != nil || gotKey != wantKey {
		t.Fatalf("full update = %x, want %x, error %v", gotKey, wantKey, err)
	}
}

func TestCommitmentEngineUpdateCommitmentRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	engine, err := NewCommitmentEngine(context.Background(), testCommitmentLimits())
	if err != nil {
		t.Fatalf("new commitment engine: %v", err)
	}
	var vector Vector
	setVectorUint64(&vector, 1, 1)
	setVectorUint64(&vector, 2, 2)
	committed, err := engine.Commit(context.Background(), vector)
	if err != nil {
		t.Fatalf("commit vector: %v", err)
	}
	valid := VectorUpdate{Index: 1, Old: vector[1]}

	var nilContext context.Context
	if _, err := engine.UpdateCommitment(nilContext, committed, nil); !errors.Is(err, errInvalidCommitmentContext) {
		t.Fatalf("nil context error = %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := engine.UpdateCommitment(cancelled, committed, nil); !errors.Is(err, errCommitmentCancelled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context error = %v", err)
	}
	var nilEngine *CommitmentEngine
	if _, err := nilEngine.UpdateCommitment(context.Background(), committed, nil); !errors.Is(err, errInvalidCommitmentEngine) {
		t.Fatalf("nil engine error = %v", err)
	}
	corruptEngine := *engine
	corruptEngine.limits = CommitmentLimits{}
	if _, err := corruptEngine.UpdateCommitment(context.Background(), committed, nil); !errors.Is(err, errInvalidCommitmentEngine) {
		t.Fatalf("corrupt engine error = %v", err)
	}
	if _, err := engine.UpdateCommitment(context.Background(), VectorCommitment{}, nil); !errors.Is(err, errInvalidCommitment) {
		t.Fatalf("invalid commitment error = %v", err)
	}
	if _, err := engine.UpdateCommitment(
		context.Background(), committed, make([]VectorUpdate, VectorWidth+1),
	); !errors.Is(err, errInvalidCommitmentUpdate) {
		t.Fatalf("oversized update error = %v", err)
	}
	if _, err := engine.UpdateCommitment(
		context.Background(), committed,
		[]VectorUpdate{valid, valid},
	); !errors.Is(err, errInvalidCommitmentUpdate) {
		t.Fatalf("duplicate position error = %v", err)
	}

	modulus, decodeErr := hex.DecodeString(
		"e1e77628b506fd747104197400878fff007668020276ce0c525f67cad469fb1c",
	)
	if decodeErr != nil {
		t.Fatalf("decode modulus: %v", decodeErr)
	}
	invalidOld := valid
	copy(invalidOld.Old[:], modulus)
	if _, err := engine.UpdateCommitment(
		context.Background(), committed, []VectorUpdate{invalidOld},
	); !errors.Is(err, errInvalidScalar) {
		t.Fatalf("invalid old scalar error = %v", err)
	}
	invalidNew := valid
	copy(invalidNew.New[:], modulus)
	if _, err := engine.UpdateCommitment(
		context.Background(), committed, []VectorUpdate{invalidNew},
	); !errors.Is(err, errInvalidScalar) {
		t.Fatalf("invalid new scalar error = %v", err)
	}
}

func TestCommitmentEngineUpdateCommitmentEnforcesResourcesAndCancellation(t *testing.T) {
	t.Parallel()
	const expectedWorkingBytes = uint64(25_600)

	engine, err := NewCommitmentEngine(context.Background(), testCommitmentLimits())
	if err != nil {
		t.Fatalf("new commitment engine: %v", err)
	}
	var vector Vector
	setVectorUint64(&vector, 1, 1)
	setVectorUint64(&vector, 2, 2)
	committed, err := engine.Commit(context.Background(), vector)
	if err != nil {
		t.Fatalf("commit vector: %v", err)
	}
	updates := []VectorUpdate{
		{Index: 1, Old: vector[1]},
		{Index: 2, Old: vector[2]},
	}

	limited := *engine
	limited.limits.MaxTemporaryBytes = expectedWorkingBytes - 1
	_, err = limited.UpdateCommitment(context.Background(), committed, updates)
	assertCommitmentResourceError(
		t, err, CommitmentResourceTemporaryBytes,
		expectedWorkingBytes-1, expectedWorkingBytes,
	)
	limited = *engine
	limited.limits.MaxScalarDecodes = 3
	_, err = limited.UpdateCommitment(context.Background(), committed, updates)
	assertCommitmentResourceError(
		t, err, CommitmentResourceScalarDecodes, 3, 4,
	)
	limited = *engine
	limited.limits.MaxMSMTerms = 1
	_, err = limited.UpdateCommitment(context.Background(), committed, updates)
	assertCommitmentResourceError(
		t, err, CommitmentResourceMSMTerms, 1, 2,
	)

	for _, cancelAt := range []int{2, 3, 4, 5} {
		ctx := &commitCancelContext{cancelAt: cancelAt}
		if _, err := engine.UpdateCommitment(
			ctx, committed, []VectorUpdate{{Index: 1, Old: vector[1]}},
		); !errors.Is(err, errCommitmentCancelled) {
			t.Fatalf("cancel at %d error = %v", cancelAt, err)
		}
	}

	noOpContext := &commitCancelContext{cancelAt: 5}
	if _, err := engine.UpdateCommitment(
		noOpContext,
		committed,
		[]VectorUpdate{{Index: 1, Old: vector[1], New: vector[1]}},
	); err != nil {
		t.Fatalf("zero-delta update spent group work: %v", err)
	}
}
