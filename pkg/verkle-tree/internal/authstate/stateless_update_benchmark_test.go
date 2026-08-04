package authstate

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
)

var benchmarkStatelessRoot backend.Root

func BenchmarkStatelessUpdaterTransitions(b *testing.B) {
	retainedDelete := testKey(0x10, 0x00)
	retainedMember := testKey(0x10, 0x01)
	replaced := testKey(0x20, 0x00)
	replacement := testKey(0x20, 0x00)
	replacement[1] = 0x30
	collapsed := testKey(0x30, 0x00)
	collapsed[1] = 0x10
	collapseSurvivor := testKey(0x30, 0x00)
	collapseSurvivor[1] = 0x20
	tests := []struct {
		name    string
		entries []Entry
		updates []Update
	}{
		{
			name: "delete-retained-member",
			entries: []Entry{
				{Key: retainedDelete, Value: testValue(1)},
				{Key: retainedMember, Value: testValue(2)},
			},
			updates: []Update{Delete(retainedDelete)},
		},
		{
			name: "replace-emptied-stem",
			entries: []Entry{
				{Key: replaced, Value: testValue(1)},
			},
			updates: []Update{
				Delete(replaced),
				Set(replacement, testValue(2)),
			},
		},
		{
			name: "collapse-unary-parent",
			entries: []Entry{
				{Key: collapsed, Value: testValue(1)},
				{Key: collapseSurvivor, Value: testValue(2)},
			},
			updates: []Update{Delete(collapsed)},
		},
	}

	for _, test := range tests {
		test := test
		b.Run(test.name, func(b *testing.B) {
			snapshot := newTestSnapshot(b, test.entries)
			openingLimits := testAuthstateAggregateOpeningLimits()
			openingLimits.MaxQueries = 4_096
			openingLimits.MaxScalarDecodes = 4_096 * backend.VectorWidth
			openingLimits.MaxMSMTerms = 8_192 * backend.VectorWidth
			proofEngine, err := NewProofEngine(context.Background(), openingLimits)
			if err != nil {
				b.Fatal(err)
			}
			proof, err := proofEngine.ProveUpdates(
				context.Background(), snapshot, test.updates,
				topologyProofGenerationLimits(),
			)
			if err != nil {
				b.Fatal(err)
			}
			updater, err := NewStatelessUpdater(
				context.Background(), openingLimits, testCommitmentLimits(),
			)
			if err != nil {
				b.Fatal(err)
			}
			verificationLimits := topologyProofVerificationLimits()
			updateLimits := topologyStatelessUpdateLimits()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				benchmarkStatelessRoot, err = updater.Apply(
					context.Background(), proof, test.updates,
					verificationLimits, updateLimits,
				)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
