package committedtree

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
)

func TestNewBuilderRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	var missingContext context.Context
	if _, err := NewBuilder(
		missingContext,
		testLimits(),
		testCommitmentLimits(),
	); !errors.Is(err, errInvalidContext) {
		t.Fatalf("nil context error = %v, want %v", err, errInvalidContext)
	}
	if _, err := NewBuilder(
		context.Background(),
		Limits{},
		testCommitmentLimits(),
	); !errors.Is(err, errInvalidLimits) {
		t.Fatalf("invalid tree limits error = %v, want %v", err, errInvalidLimits)
	}
	if _, err := NewBuilder(
		context.Background(),
		testLimits(),
		backend.CommitmentLimits{},
	); err == nil {
		t.Fatal("invalid commitment limits were accepted")
	}
}

func TestBuilderZeroAndCorruptValuesRejectUse(t *testing.T) {
	t.Parallel()

	var nilBuilder *Builder
	if _, err := nilBuilder.Build(context.Background(), nil); !errors.Is(err, errInvalidBuilder) {
		t.Fatalf("nil builder error = %v, want %v", err, errInvalidBuilder)
	}
	var zero Builder
	if _, err := zero.Build(context.Background(), nil); !errors.Is(err, errInvalidBuilder) {
		t.Fatalf("zero builder error = %v, want %v", err, errInvalidBuilder)
	}
	corrupt := Builder{limits: testLimits(), valid: true}
	if _, err := corrupt.Build(context.Background(), nil); !errors.Is(err, errInvalidBuilder) {
		t.Fatalf("nil engine error = %v, want %v", err, errInvalidBuilder)
	}
	corrupt.engine = &scriptedCommitmentEngine{}
	corrupt.limits = Limits{}
	if _, err := corrupt.Build(context.Background(), nil); !errors.Is(err, errInvalidBuilder) {
		t.Fatalf("corrupt limits error = %v, want %v", err, errInvalidBuilder)
	}
}

func TestBuilderPropagatesPreparationFailure(t *testing.T) {
	t.Parallel()

	builder := &Builder{
		limits: testLimits(),
		engine: &scriptedCommitmentEngine{},
		valid:  true,
	}
	entry := Entry{Key: testKey(1, 2), Value: testValue(3)}
	if _, err := builder.Build(
		context.Background(),
		[]Entry{entry, entry},
	); !errors.Is(err, errDuplicateKey) {
		t.Fatalf("preparation error = %v, want %v", err, errDuplicateKey)
	}
}

func TestPrepareBuildPropagatesEveryCancellationBoundary(t *testing.T) {
	t.Parallel()

	entries := []Entry{
		{Key: testKey(2, 0), Value: testValue(2)},
		{Key: testKey(1, 0), Value: testValue(1)},
	}
	for cancelAt := 1; cancelAt <= 24; cancelAt++ {
		_, err := prepareBuild(&cancelContext{cancelAt: cancelAt}, entries, testLimits())
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel at %d error = %v, want cancellation", cancelAt, err)
		}
	}
}

func TestPrepareBuildRejectsIntermediateScratchBudget(t *testing.T) {
	t.Parallel()

	entries := []Entry{
		{Key: testKey(0, 0), Value: testValue(1)},
		{Key: testKey(1, 0), Value: testValue(2)},
	}
	limits := testLimits()
	limits.MaxTemporaryBytes = 383
	_, err := prepareBuild(context.Background(), entries, limits)
	assertResourceError(t, err, ResourceTemporaryBytes, 383, 384)
}

func TestExactConfiguredBoundariesAreAccepted(t *testing.T) {
	t.Parallel()

	limits := Limits{
		MaxEntries:         maxSupportedCount,
		MaxStems:           maxSupportedCount,
		MaxNodes:           maxSupportedCount,
		MaxEdges:           maxSupportedCount,
		MaxCommitments:     maxSupportedCount,
		MaxFieldMappings:   1,
		MaxCommitmentTerms: 1,
		MaxTemporaryBytes:  1,
	}
	if err := limits.validate(); err != nil {
		t.Fatalf("exact maximum limits: %v", err)
	}
	if err := checkResource(ResourceEntries, 7, 7); err != nil {
		t.Fatalf("exact resource limit: %v", err)
	}
}

func TestPrepareBuildBoundsAggregateCryptographicWork(t *testing.T) {
	t.Parallel()

	entries := []Entry{
		{Key: testKey(0, 0), Value: testValue(1)},
		{Key: testKey(1, 0), Value: testValue(2)},
	}
	limits := testLimits()
	limits.MaxFieldMappings = 5
	_, err := prepareBuild(context.Background(), entries, limits)
	assertResourceError(t, err, ResourceFieldMappings, 5, 6)

	limits = testLimits()
	limits.MaxCommitmentTerms = 13
	_, err = prepareBuild(context.Background(), entries, limits)
	assertResourceError(t, err, ResourceCommitmentTerms, 13, 14)
}

func TestContextAwareConstructionHelpersCancel(t *testing.T) {
	t.Parallel()

	entries := []Entry{
		{Key: testKey(2, 0), Value: testValue(2)},
		{Key: testKey(1, 0), Value: testValue(1)},
	}
	if err := sortEntries(&cancelContext{cancelAt: 1}, entries); !errors.Is(err, context.Canceled) {
		t.Fatalf("sort outer cancellation error = %v", err)
	}
	two := []Entry{
		{Key: testKey(2, 0), Value: testValue(2)},
		{Key: testKey(1, 0), Value: testValue(1)},
	}
	if err := sortEntries(&cancelContext{cancelAt: 4}, two); !errors.Is(err, context.Canceled) {
		t.Fatalf("sort copy cancellation error = %v", err)
	}
	equalKey := testKey(9, 9)
	stable := []Entry{
		{Key: equalKey, Value: Value{0: 1}},
		{Key: testKey(8, 8), Value: Value{0: 2}},
		{Key: equalKey, Value: Value{0: 3}},
	}
	if err := sortEntries(context.Background(), stable); err != nil {
		t.Fatalf("stable sort: %v", err)
	}
	if stable[0].Key != testKey(8, 8) ||
		stable[1].Value[0] != 1 ||
		stable[2].Value[0] != 3 {
		t.Fatalf("stable ordering = %#v", stable)
	}
	if _, err := countStems(&cancelContext{cancelAt: 1}, entries); !errors.Is(err, context.Canceled) {
		t.Fatalf("stem count cancellation error = %v", err)
	}
	if _, err := groupEntries(
		&cancelContext{cancelAt: 1},
		entries,
		2,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("group outer cancellation error = %v", err)
	}
	sameStem := []Entry{
		{Key: testKey(0, 0), Value: testValue(1)},
		{Key: testKey(0, 1), Value: testValue(2)},
	}
	if _, err := groupEntries(
		&cancelContext{cancelAt: 2},
		sameStem,
		1,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("group inner cancellation error = %v", err)
	}

	groups := []stemGroup{{stem: [31]byte{0}}, {stem: [31]byte{1}}}
	counts := topologyCounts{stems: 2, internalNodes: 1}
	if err := countInternalNodes(
		&cancelContext{cancelAt: 1},
		groups,
		0,
		&counts,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("topology outer cancellation error = %v", err)
	}
	groups[1].stem[0] = 0
	groups[1].stem[1] = 1
	counts = topologyCounts{stems: 2, internalNodes: 1}
	if err := countInternalNodes(
		&cancelContext{cancelAt: 2},
		groups,
		0,
		&counts,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("topology recursive cancellation error = %v", err)
	}
}

func TestBuildPreparedPropagatesCommitmentFailure(t *testing.T) {
	t.Parallel()

	plan, err := prepareBuild(
		context.Background(),
		[]Entry{{Key: testKey(1, 2), Value: testValue(3)}},
		testLimits(),
	)
	if err != nil {
		t.Fatalf("prepare build: %v", err)
	}
	want := errors.New("commitment failed")
	if _, err := buildPrepared(
		context.Background(),
		plan,
		&scriptedCommitmentEngine{results: []commitResult{{err: want}}},
	); !errors.Is(err, want) {
		t.Fatalf("construction error = %v, want %v", err, want)
	}
}

func TestTreeBuilderPropagatesCommitAndMappingFailures(t *testing.T) {
	t.Parallel()

	valid := validVectorCommitment(t)
	entry := Entry{Key: testKey(0, 0), Value: Value{}}
	group := stemGroup{entryStart: 0, entryEnd: 1}
	want := errors.New("scripted failure")
	tests := []struct {
		name    string
		results []commitResult
		method  string
	}{
		{name: "c1 commit", results: []commitResult{{err: want}}, method: "stem"},
		{name: "c2 commit", results: []commitResult{{commitment: valid}, {err: want}}, method: "stem"},
		{name: "c1 mapping", results: []commitResult{{}, {commitment: valid}}, method: "stem"},
		{name: "c2 mapping", results: []commitResult{{commitment: valid}, {}}, method: "stem"},
		{name: "stem commit", results: []commitResult{{commitment: valid}, {commitment: valid}, {err: want}}, method: "stem"},
		{name: "child commit", results: []commitResult{{err: want}}, method: "internal"},
		{name: "child mapping", results: []commitResult{{commitment: valid}, {commitment: valid}, {}}, method: "internal"},
		{name: "root commit", results: []commitResult{{commitment: valid}, {commitment: valid}, {commitment: valid}, {err: want}}, method: "internal"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine := &scriptedCommitmentEngine{results: test.results}
			builder := treeBuilder{
				ctx:     context.Background(),
				entries: []Entry{entry},
				groups:  []stemGroup{group},
				engine:  engine,
			}
			var err error
			if test.method == "stem" {
				_, err = builder.commitStem(group, 1)
			} else {
				_, err = builder.commitInternal(0, 1, 0)
			}
			if err == nil {
				t.Fatal("scripted failure was not propagated")
			}
		})
	}
}

func TestTreeBuilderChecksContext(t *testing.T) {
	t.Parallel()

	builder := treeBuilder{
		ctx:     &cancelContext{cancelAt: 1},
		entries: []Entry{{}},
		groups:  []stemGroup{{entryEnd: 1}},
		engine:  &scriptedCommitmentEngine{},
	}
	if _, err := builder.commitInternal(0, 1, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("internal cancellation error = %v", err)
	}
	builder.ctx = &cancelContext{cancelAt: 2}
	if _, err := builder.commitInternal(0, 1, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("internal construction cancellation error = %v", err)
	}
	builder.ctx = &cancelContext{cancelAt: 1}
	if _, err := builder.commitStem(builder.groups[0], 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("stem cancellation error = %v", err)
	}
}

type commitResult struct {
	commitment backend.VectorCommitment
	err        error
}

type scriptedCommitmentEngine struct {
	results []commitResult
	calls   int
}

func (engine *scriptedCommitmentEngine) Commit(
	context.Context,
	backend.Vector,
) (backend.VectorCommitment, error) {
	if engine.calls >= len(engine.results) {
		return backend.VectorCommitment{}, errors.New("unexpected commitment call")
	}
	result := engine.results[engine.calls]
	engine.calls++

	return result.commitment, result.err
}

func validVectorCommitment(t testing.TB) backend.VectorCommitment {
	t.Helper()

	engine, err := backend.NewCommitmentEngine(
		context.Background(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("new commitment engine: %v", err)
	}
	var vector backend.Vector
	vector[0][0] = 1
	committed, err := engine.Commit(context.Background(), vector)
	if err != nil {
		t.Fatalf("commit valid vector: %v", err)
	}

	return committed
}
