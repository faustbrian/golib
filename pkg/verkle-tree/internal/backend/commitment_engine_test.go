package backend

import (
	"context"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/crate-crypto/go-ipa/ipa"
)

func TestCommitmentEngineCommitsZeroVectorAsInternalIdentity(t *testing.T) {
	t.Parallel()

	direct := EmptyVectorCommitment()
	if identity, err := direct.IsIdentity(); err != nil || !identity {
		t.Fatalf("empty vector identity = %t, error %v", identity, err)
	}

	engine, err := NewCommitmentEngine(context.Background(), testCommitmentLimits())
	if err != nil {
		t.Fatalf("new commitment engine: %v", err)
	}
	committed, err := engine.Commit(context.Background(), Vector{})
	if err != nil {
		t.Fatalf("commit zero vector: %v", err)
	}
	identity, err := committed.IsIdentity()
	if err != nil {
		t.Fatalf("classify zero commitment: %v", err)
	}
	if !identity {
		t.Fatal("zero vector commitment is not the internal identity")
	}
	if _, err := committed.Bytes(); !errors.Is(err, errInvalidCommitment) {
		t.Fatalf("encode identity error = %v, want %v", err, errInvalidCommitment)
	}
	got, err := committed.ScalarBytes()
	if err != nil {
		t.Fatalf("map identity commitment: %v", err)
	}
	if got != ([scalarSize]byte{}) {
		t.Fatalf("identity scalar = %x, want zero", got)
	}
}

func TestCommitmentEngineConcurrentCommitsAreDeterministic(t *testing.T) {
	t.Parallel()

	engine, err := NewCommitmentEngine(context.Background(), testCommitmentLimits())
	if err != nil {
		t.Fatalf("new commitment engine: %v", err)
	}
	vector, _ := commitmentFixtureVector(t, "sparse-boundaries")
	want, err := engine.Commit(context.Background(), vector)
	if err != nil {
		t.Fatalf("commit expected vector: %v", err)
	}
	wantBytes, err := want.Bytes()
	if err != nil {
		t.Fatalf("encode expected commitment: %v", err)
	}

	const workers = 8
	errorsByWorker := make(chan error, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for range workers {
		go func() {
			defer group.Done()
			got, commitErr := engine.Commit(context.Background(), vector)
			if commitErr != nil {
				errorsByWorker <- commitErr
				return
			}
			gotBytes, encodeErr := got.Bytes()
			if encodeErr != nil {
				errorsByWorker <- encodeErr
				return
			}
			if gotBytes != wantBytes {
				errorsByWorker <- errors.New("concurrent commitment differs")
			}
		}()
	}
	group.Wait()
	close(errorsByWorker)
	for workerErr := range errorsByWorker {
		t.Error(workerErr)
	}
}

func TestCommitmentEngineChecksCancellationDuringCommitWork(t *testing.T) {
	t.Parallel()

	engine, err := NewCommitmentEngine(context.Background(), testCommitmentLimits())
	if err != nil {
		t.Fatalf("new commitment engine: %v", err)
	}
	var vector Vector
	setVectorUint64(&vector, 0, 1)
	for _, cancelAt := range []int{3, VectorWidth + 2, VectorWidth + 3} {
		ctx := &commitCancelContext{cancelAt: cancelAt}
		if _, err := engine.Commit(ctx, vector); !errors.Is(err, errCommitmentCancelled) {
			t.Fatalf("cancel at %d error = %v, want cancellation", cancelAt, err)
		}
	}
}

func TestCommitmentEngineRejectsGeneratorSubstitution(t *testing.T) {
	t.Parallel()

	generators := ipa.GenerateRandomPoints(VectorWidth)
	if err := validateGeneratorSet(generators[:VectorWidth-1]); !errors.Is(err, errGeneratorMismatch) {
		t.Fatalf("short generator set error = %v, want %v", err, errGeneratorMismatch)
	}
	if err := validateGeneratorSet(generators); err != nil {
		t.Fatalf("validate pinned generator set: %v", err)
	}
	generators[0] = generators[1]
	if err := validateGeneratorSet(generators); !errors.Is(err, errGeneratorMismatch) {
		t.Fatalf("substituted generator error = %v, want %v", err, errGeneratorMismatch)
	}
	if _, err := newCommitmentEngineFromGenerators(
		context.Background(),
		testCommitmentLimits(),
		generators,
	); !errors.Is(err, errGeneratorMismatch) {
		t.Fatalf("substituted engine error = %v, want %v", err, errGeneratorMismatch)
	}
}

func TestCommitmentEngineRejectsInvalidAndExhaustedLimits(t *testing.T) {
	t.Parallel()

	invalid := []CommitmentLimits{
		{},
		{
			MaxScalarDecodes:  1,
			MaxMSMTerms:       1,
			MaxTemporaryBytes: 1,
		},
		{
			MaxGeneratorDerivations: 1,
			MaxMSMTerms:             1,
			MaxTemporaryBytes:       1,
		},
		{
			MaxGeneratorDerivations: 1,
			MaxScalarDecodes:        1,
			MaxTemporaryBytes:       1,
		},
		{
			MaxGeneratorDerivations: 1,
			MaxScalarDecodes:        1,
			MaxMSMTerms:             1,
		},
	}
	for _, limits := range invalid {
		if _, err := NewCommitmentEngine(context.Background(), limits); !errors.Is(err, errInvalidCommitmentLimits) {
			t.Fatalf("invalid limits error = %v, want %v", err, errInvalidCommitmentLimits)
		}
	}

	limits := testCommitmentLimits()
	limits.MaxGeneratorDerivations = VectorWidth - 1
	_, err := NewCommitmentEngine(context.Background(), limits)
	assertCommitmentResourceError(
		t,
		err,
		CommitmentResourceGeneratorDerivations,
		VectorWidth-1,
		VectorWidth,
	)
	limits = testCommitmentLimits()
	limits.MaxTemporaryBytes = VectorWidth*generatorWorkingBytes - 1
	_, err = NewCommitmentEngine(context.Background(), limits)
	assertCommitmentResourceError(
		t,
		err,
		CommitmentResourceTemporaryBytes,
		VectorWidth*generatorWorkingBytes-1,
		VectorWidth*generatorWorkingBytes,
	)
}

func TestCommitmentEngineRejectsInvalidScalarsAndCommitBudgets(t *testing.T) {
	t.Parallel()

	engine, err := NewCommitmentEngine(context.Background(), testCommitmentLimits())
	if err != nil {
		t.Fatalf("new commitment engine: %v", err)
	}
	var vector Vector
	setVectorUint64(&vector, 0, 1)
	setVectorUint64(&vector, 1, 2)

	limited := *engine
	limited.limits.MaxTemporaryBytes = 8_703
	_, err = limited.Commit(context.Background(), vector)
	assertCommitmentResourceError(
		t,
		err,
		CommitmentResourceTemporaryBytes,
		8_703,
		8_704,
	)
	limited = *engine
	limited.limits.MaxScalarDecodes = 1
	_, err = limited.Commit(context.Background(), vector)
	assertCommitmentResourceError(
		t,
		err,
		CommitmentResourceScalarDecodes,
		1,
		2,
	)
	limited = *engine
	limited.limits.MaxMSMTerms = 1
	_, err = limited.Commit(context.Background(), vector)
	assertCommitmentResourceError(
		t,
		err,
		CommitmentResourceMSMTerms,
		1,
		2,
	)

	modulus, decodeErr := hex.DecodeString(
		"e1e77628b506fd747104197400878fff007668020276ce0c525f67cad469fb1c",
	)
	if decodeErr != nil {
		t.Fatalf("decode modulus: %v", decodeErr)
	}
	copy(vector[0][:], modulus)
	if _, err := engine.Commit(context.Background(), vector); !errors.Is(err, errInvalidScalar) {
		t.Fatalf("non-canonical scalar error = %v, want %v", err, errInvalidScalar)
	}

	var nilEngine *CommitmentEngine
	if _, err := nilEngine.Commit(context.Background(), Vector{}); !errors.Is(err, errInvalidCommitmentEngine) {
		t.Fatalf("nil engine error = %v, want %v", err, errInvalidCommitmentEngine)
	}
	var zeroEngine CommitmentEngine
	if _, err := zeroEngine.Commit(context.Background(), Vector{}); !errors.Is(err, errInvalidCommitmentEngine) {
		t.Fatalf("zero engine error = %v, want %v", err, errInvalidCommitmentEngine)
	}
	corrupt := *engine
	corrupt.limits = CommitmentLimits{}
	if _, err := corrupt.Commit(context.Background(), Vector{}); !errors.Is(err, errInvalidCommitmentEngine) {
		t.Fatalf("corrupt engine error = %v, want %v", err, errInvalidCommitmentEngine)
	}
}

func TestCommitmentEngineRejectsNilAndCancelledContexts(t *testing.T) {
	t.Parallel()

	var missingContext context.Context
	if _, err := NewCommitmentEngine(missingContext, testCommitmentLimits()); !errors.Is(err, errInvalidCommitmentContext) {
		t.Fatalf("nil constructor context error = %v, want %v", err, errInvalidCommitmentContext)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewCommitmentEngine(cancelled, testCommitmentLimits()); !errors.Is(err, context.Canceled) || !errors.Is(err, errCommitmentCancelled) {
		t.Fatalf("cancelled constructor error = %v, want cancellation", err)
	}
	if _, err := NewCommitmentEngine(
		&commitCancelContext{cancelAt: 2},
		testCommitmentLimits(),
	); !errors.Is(err, errCommitmentCancelled) {
		t.Fatalf("post-derivation cancellation error = %v, want cancellation", err)
	}

	engine, err := NewCommitmentEngine(context.Background(), testCommitmentLimits())
	if err != nil {
		t.Fatalf("new commitment engine: %v", err)
	}
	if _, err := engine.Commit(missingContext, Vector{}); !errors.Is(err, errInvalidCommitmentContext) {
		t.Fatalf("nil commit context error = %v, want %v", err, errInvalidCommitmentContext)
	}
	if _, err := engine.Commit(cancelled, Vector{}); !errors.Is(err, context.Canceled) || !errors.Is(err, errCommitmentCancelled) {
		t.Fatalf("cancelled commit error = %v, want cancellation", err)
	}
}

func TestVectorCommitmentZeroValueRejectsUse(t *testing.T) {
	t.Parallel()

	var committed VectorCommitment
	if _, err := committed.IsIdentity(); !errors.Is(err, errInvalidCommitment) {
		t.Fatalf("classify zero value error = %v, want %v", err, errInvalidCommitment)
	}
	if _, err := committed.Bytes(); !errors.Is(err, errInvalidCommitment) {
		t.Fatalf("encode zero value error = %v, want %v", err, errInvalidCommitment)
	}
	if _, err := committed.ScalarBytes(); !errors.Is(err, errInvalidCommitment) {
		t.Fatalf("map zero value error = %v, want %v", err, errInvalidCommitment)
	}
}

func TestVectorCommitmentDeduplicationKey(t *testing.T) {
	t.Parallel()

	if _, err := (VectorCommitment{}).DeduplicationKey(); !errors.Is(err, errInvalidCommitment) {
		t.Fatalf("invalid key error = %v", err)
	}
	identityKey, err := EmptyVectorCommitment().DeduplicationKey()
	if err != nil || identityKey != ([CommitmentSize]byte{}) {
		t.Fatalf("identity key = %x, error = %v", identityKey, err)
	}
	engine, err := NewCommitmentEngine(context.Background(), testCommitmentLimits())
	if err != nil {
		t.Fatalf("new commitment engine: %v", err)
	}
	var vector Vector
	vector[0][0] = 1
	commitment, err := engine.Commit(context.Background(), vector)
	if err != nil {
		t.Fatalf("commit vector: %v", err)
	}
	key, err := commitment.DeduplicationKey()
	if err != nil {
		t.Fatalf("deduplication key: %v", err)
	}
	encoded, err := commitment.Bytes()
	if err != nil || key != encoded {
		t.Fatalf("key = %x, encoding = %x, error = %v", key, encoded, err)
	}
}

func testCommitmentLimits() CommitmentLimits {
	return CommitmentLimits{
		MaxGeneratorDerivations: VectorWidth,
		MaxScalarDecodes:        VectorWidth,
		MaxMSMTerms:             VectorWidth,
		MaxTemporaryBytes:       1 << 20,
	}
}

func assertCommitmentResourceError(
	t *testing.T,
	err error,
	resource CommitmentResource,
	limit uint64,
	actual uint64,
) {
	t.Helper()

	var resourceErr *CommitmentResourceError
	if !errors.As(err, &resourceErr) {
		t.Fatalf("error = %v, want CommitmentResourceError", err)
	}
	if resourceErr.Resource != resource ||
		resourceErr.Limit != limit ||
		resourceErr.Actual != actual {
		t.Fatalf(
			"resource error = %#v, want resource %d limit %d actual %d",
			resourceErr,
			resource,
			limit,
			actual,
		)
	}
	if !errors.Is(err, errCommitmentResource) || resourceErr.Error() == "" {
		t.Fatalf("resource error does not expose sentinel: %v", err)
	}
}

type commitCancelContext struct {
	calls    int
	cancelAt int
}

func (*commitCancelContext) Deadline() (time.Time, bool) {
	return time.Time{}, false
}

func (*commitCancelContext) Done() <-chan struct{} {
	return nil
}

func (ctx *commitCancelContext) Err() error {
	ctx.calls++
	if ctx.calls >= ctx.cancelAt {
		return context.Canceled
	}

	return nil
}

func (*commitCancelContext) Value(any) any {
	return nil
}
