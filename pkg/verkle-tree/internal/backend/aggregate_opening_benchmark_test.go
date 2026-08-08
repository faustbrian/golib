package backend

import (
	"context"
	"strconv"
	"testing"
)

var benchmarkAggregateOpeningProof OpeningProof

func BenchmarkAggregateOpeningEngineOpenSharedVectors(b *testing.B) {
	for _, vectorCount := range []int{1, 2} {
		b.Run(strconv.Itoa(vectorCount), func(b *testing.B) {
			engine, queries := benchmarkAggregateOpeningQueries(b, vectorCount)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				proof, err := engine.Open(context.Background(), queries)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkAggregateOpeningProof = proof
			}
			b.ReportMetric(float64(len(queries)), "queries/op")
		})
	}
}

func benchmarkAggregateOpeningQueries(
	t testing.TB,
	vectorCount int,
) (*AggregateOpeningEngine, []AggregateProverQuery) {
	t.Helper()

	queryCount := uint64(vectorCount * VectorWidth)
	limits := testAggregateOpeningLimits()
	limits.MaxQueries = uint32(queryCount)
	limits.MaxScalarDecodes = queryCount * VectorWidth
	limits.MaxMSMTerms = queryCount*VectorWidth + aggregateFixedMSMTerms
	limits.MaxTemporaryBytes = max(
		aggregateSetupWorkingBytes,
		queryCount*aggregateQueryWorkingBytes,
	)
	engine, err := NewAggregateOpeningEngine(context.Background(), limits)
	if err != nil {
		t.Fatalf("new aggregate opening engine: %v", err)
	}
	commitmentEngine, err := NewCommitmentEngine(
		context.Background(),
		testCommitmentLimits(),
	)
	if err != nil {
		t.Fatalf("new commitment engine: %v", err)
	}
	queries := make([]AggregateProverQuery, 0, int(queryCount))
	for vectorIndex := range vectorCount {
		vector := new(Vector)
		for index := range VectorWidth {
			setVectorUint64(
				vector,
				index,
				uint64(vectorIndex+1)*uint64(index+1),
			)
		}
		commitment, commitErr := commitmentEngine.Commit(
			context.Background(),
			*vector,
		)
		if commitErr != nil {
			t.Fatalf("commit vector %d: %v", vectorIndex, commitErr)
		}
		for index := range VectorWidth {
			queries = append(queries, AggregateProverQuery{
				Commitment: commitment,
				Vector:     vector,
				Index:      uint8(index),
			})
		}
	}

	return engine, queries
}
