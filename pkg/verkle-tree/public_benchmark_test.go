package verkletree_test

import (
	"context"
	"testing"

	verkletree "github.com/faustbrian/golib/pkg/verkle-tree"
)

var benchmarkPublicStatelessEngine verkletree.StatelessEngine

func BenchmarkPublicSnapshotOperations(b *testing.B) {
	ctx := context.Background()
	profile := verkletree.BandersnatchIPA256V0()
	limits := publicSnapshotLimits()
	entries := benchmarkPublicEntries()
	snapshot, err := verkletree.NewSnapshot(ctx, profile, entries, limits)
	if err != nil {
		b.Fatalf("prepare snapshot: %v", err)
	}

	present := entries[0].Key
	insertSuffix := publicKey(present[0], 0xf0)
	insertStem := publicKey(0xf0, 0x01)
	absent := publicKey(0xff, 0xff)
	batch := benchmarkPublicBatch()

	b.Run("construct-ordered-32", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(entries) * (len(verkletree.Key{}) + len(verkletree.Value{}))))
		for b.Loop() {
			if _, err := verkletree.NewSnapshot(ctx, profile, entries, limits); err != nil {
				b.Fatal(err)
			}
		}
		benchmarkPublicReportThroughput(b)
	})
	b.Run("get-present", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, present, err := snapshot.Get(ctx, present); err != nil || !present {
				b.Fatalf("get = (%t, %v)", present, err)
			}
		}
		benchmarkPublicReportThroughput(b)
	})
	b.Run("get-present-parallel", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if _, found, err := snapshot.Get(ctx, present); err != nil || !found {
					b.Fatalf("get = (%t, %v)", found, err)
				}
			}
		})
		benchmarkPublicReportThroughput(b)
	})
	b.Run("insert-existing-stem", func(b *testing.B) {
		updates := []verkletree.Update{
			verkletree.Set(insertSuffix, publicValue(0xa1)),
		}
		benchmarkPublicApply(b, snapshot, updates)
	})
	b.Run("insert-new-stem", func(b *testing.B) {
		updates := []verkletree.Update{
			verkletree.Set(insertStem, publicValue(0xa2)),
		}
		benchmarkPublicApply(b, snapshot, updates)
	})
	b.Run("update-present", func(b *testing.B) {
		updates := []verkletree.Update{
			verkletree.Set(present, publicValue(0xa3)),
		}
		benchmarkPublicApply(b, snapshot, updates)
	})
	b.Run("delete-present", func(b *testing.B) {
		updates := []verkletree.Update{verkletree.Delete(present)}
		benchmarkPublicApply(b, snapshot, updates)
	})
	b.Run("delete-absent", func(b *testing.B) {
		updates := []verkletree.Update{verkletree.Delete(absent)}
		benchmarkPublicApply(b, snapshot, updates)
	})
	b.Run("batch-mixed-16", func(b *testing.B) {
		benchmarkPublicApply(b, snapshot, batch)
	})
}

func BenchmarkPublicProofOperations(b *testing.B) {
	ctx := context.Background()
	snapshot := benchmarkPublicState(b)
	engine, err := verkletree.NewProofEngine(
		ctx,
		verkletree.BandersnatchIPA256V0(),
		publicOpeningLimits(),
	)
	if err != nil {
		b.Fatalf("prepare proof engine: %v", err)
	}

	membership := []verkletree.Key{publicKey(0x00, 0x00)}
	nonMembership := []verkletree.Key{publicKey(0x00, 0xf0)}
	aggregate := []verkletree.Key{
		publicKey(0x00, 0x00),
		publicKey(0x01, 0x00),
		publicKey(0x02, 0x00),
		publicKey(0x03, 0x00),
		publicKey(0x00, 0xf0),
		publicKey(0x01, 0xf1),
		publicKey(0x80, 0x00),
		publicKey(0x81, 0x00),
	}
	membershipProof := benchmarkPublicProof(b, engine, snapshot, membership)
	nonMembershipProof := benchmarkPublicProof(b, engine, snapshot, nonMembership)
	aggregateProof := benchmarkPublicProof(b, engine, snapshot, aggregate)
	membershipBytes, err := membershipProof.Bytes(ctx, publicProofEncodingLimits())
	if err != nil {
		b.Fatalf("encode membership proof: %v", err)
	}
	nonMembershipBytes, err := nonMembershipProof.Bytes(ctx, publicProofEncodingLimits())
	if err != nil {
		b.Fatalf("encode non-membership proof: %v", err)
	}
	aggregateBytes, err := aggregateProof.Bytes(ctx, publicProofEncodingLimits())
	if err != nil {
		b.Fatalf("encode aggregate proof: %v", err)
	}
	malformed := aggregateBytes[:len(aggregateBytes)-1]

	b.Run("generate-membership-1", func(b *testing.B) {
		benchmarkPublicProve(b, engine, snapshot, membership)
	})
	b.Run("verify-membership-1", func(b *testing.B) {
		benchmarkPublicVerify(b, engine, membershipProof, len(membershipBytes))
	})
	b.Run("generate-nonmembership-1", func(b *testing.B) {
		benchmarkPublicProve(b, engine, snapshot, nonMembership)
	})
	b.Run("verify-nonmembership-1", func(b *testing.B) {
		benchmarkPublicVerify(b, engine, nonMembershipProof, len(nonMembershipBytes))
	})
	b.Run("generate-aggregate-8", func(b *testing.B) {
		benchmarkPublicProve(b, engine, snapshot, aggregate)
	})
	b.Run("verify-aggregate-8", func(b *testing.B) {
		benchmarkPublicVerify(b, engine, aggregateProof, len(aggregateBytes))
	})
	b.Run("verify-aggregate-8-parallel", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if err := engine.Verify(
					ctx, aggregateProof, publicProofVerificationLimits(),
				); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.ReportMetric(float64(len(aggregateBytes)), "proof-bytes/op")
		benchmarkPublicReportThroughput(b)
	})
	b.Run("encode-aggregate-8", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(aggregateBytes)))
		for b.Loop() {
			if _, err := aggregateProof.Bytes(ctx, publicProofEncodingLimits()); err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(float64(len(aggregateBytes)), "proof-bytes/op")
		benchmarkPublicReportThroughput(b)
	})
	b.Run("decode-aggregate-8", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(aggregateBytes)))
		for b.Loop() {
			if _, err := verkletree.DecodeProof(
				ctx, aggregateBytes, publicProofDecodingLimits(),
			); err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(float64(len(aggregateBytes)), "proof-bytes/op")
		benchmarkPublicReportThroughput(b)
	})
	b.Run("reject-truncated-aggregate-8", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(malformed)))
		for b.Loop() {
			if _, err := verkletree.DecodeProof(
				ctx, malformed, publicProofDecodingLimits(),
			); err == nil {
				b.Fatal("truncated proof accepted")
			}
		}
		benchmarkPublicReportThroughput(b)
	})
}

func BenchmarkPublicStatelessWitnessOperations(b *testing.B) {
	ctx := context.Background()
	snapshot := benchmarkPublicState(b)
	proofEngine, err := verkletree.NewProofEngine(
		ctx,
		verkletree.BandersnatchIPA256V0(),
		publicOpeningLimits(),
	)
	if err != nil {
		b.Fatalf("prepare proof engine: %v", err)
	}
	updates := []verkletree.Update{
		verkletree.Set(publicKey(0x00, 0x00), publicValue(0xa1)),
		verkletree.Set(publicKey(0x00, 0xf0), publicValue(0xa2)),
	}
	proof, err := proofEngine.ProveUpdates(
		ctx, snapshot, updates, publicProofGenerationLimits(),
	)
	if err != nil {
		b.Fatalf("prepare update proof: %v", err)
	}
	next, _, err := snapshot.Apply(ctx, updates)
	if err != nil {
		b.Fatalf("prepare post-state: %v", err)
	}
	postRoot, err := next.Root()
	if err != nil {
		b.Fatalf("prepare post-root: %v", err)
	}
	witness, err := verkletree.NewWitness(
		ctx, proof, updates, postRoot, publicWitnessLimits(),
	)
	if err != nil {
		b.Fatalf("prepare witness: %v", err)
	}
	encoded, err := witness.Bytes(ctx, publicWitnessEncodingLimits())
	if err != nil {
		b.Fatalf("encode witness: %v", err)
	}
	engine, err := verkletree.NewStatelessEngineFromProofEngine(
		ctx,
		proofEngine,
		publicSnapshotLimits().Commitment,
	)
	if err != nil {
		b.Fatalf("prepare stateless engine: %v", err)
	}

	b.Run("construct-2", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := verkletree.NewWitness(
				ctx, proof, updates, postRoot, publicWitnessLimits(),
			); err != nil {
				b.Fatal(err)
			}
		}
		benchmarkPublicReportThroughput(b)
	})
	b.Run("encode-2", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(encoded)))
		for b.Loop() {
			if _, err := witness.Bytes(ctx, publicWitnessEncodingLimits()); err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(float64(len(encoded)), "witness-bytes/op")
		benchmarkPublicReportThroughput(b)
	})
	b.Run("decode-2", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(encoded)))
		for b.Loop() {
			if _, err := verkletree.DecodeWitness(
				ctx, encoded, publicWitnessDecodingLimits(),
			); err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(float64(len(encoded)), "witness-bytes/op")
		benchmarkPublicReportThroughput(b)
	})
	b.Run("verify-and-apply-2", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := engine.Apply(
				ctx,
				witness,
				publicProofVerificationLimits(),
				publicStatelessUpdateLimits(),
			); err != nil {
				b.Fatal(err)
			}
		}
		b.ReportMetric(float64(len(encoded)), "witness-bytes/op")
		benchmarkPublicReportThroughput(b)
	})
}

func BenchmarkPublicStatelessEngineInitialization(b *testing.B) {
	ctx := context.Background()
	profile := verkletree.BandersnatchIPA256V0()
	openingLimits := publicOpeningLimits()
	commitmentLimits := publicSnapshotLimits().Commitment
	proofEngine, err := verkletree.NewProofEngine(
		ctx,
		profile,
		openingLimits,
	)
	if err != nil {
		b.Fatalf("prepare proof engine: %v", err)
	}

	b.Run("fresh", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			engine, engineErr := verkletree.NewStatelessEngine(
				ctx,
				profile,
				openingLimits,
				commitmentLimits,
			)
			if engineErr != nil {
				b.Fatal(engineErr)
			}
			benchmarkPublicStatelessEngine = engine
		}
	})
	b.Run("reuse-proof-engine", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			engine, engineErr := verkletree.NewStatelessEngineFromProofEngine(
				ctx,
				proofEngine,
				commitmentLimits,
			)
			if engineErr != nil {
				b.Fatal(engineErr)
			}
			benchmarkPublicStatelessEngine = engine
		}
	})
}

func BenchmarkPublicStatelessTopologyTransitions(b *testing.B) {
	ctx := context.Background()
	profile := verkletree.BandersnatchIPA256V0()
	openingLimits := publicTopologyOpeningLimits()
	proofEngine, err := verkletree.NewProofEngine(ctx, profile, openingLimits)
	if err != nil {
		b.Fatalf("prepare topology proof engine: %v", err)
	}
	statelessEngine, err := verkletree.NewStatelessEngineFromProofEngine(
		ctx, proofEngine, publicSnapshotLimits().Commitment,
	)
	if err != nil {
		b.Fatalf("prepare topology stateless engine: %v", err)
	}

	retainedDelete := publicKey(0x10, 0x00)
	retainedMember := publicKey(0x10, 0x01)
	replaced := publicKey(0x20, 0x00)
	replaced[1] = 0x10
	replacement := publicKey(0x20, 0x00)
	replacement[1] = 0x30
	collapsed := publicKey(0x30, 0x00)
	collapsed[1] = 0x10
	collapseSurvivor := publicKey(0x30, 0x00)
	collapseSurvivor[1] = 0x20
	tests := []struct {
		name    string
		entries []verkletree.Entry
		updates []verkletree.Update
	}{
		{
			name: "delete-retained-member",
			entries: []verkletree.Entry{
				{Key: retainedDelete, Value: publicValue(1)},
				{Key: retainedMember, Value: publicValue(2)},
			},
			updates: []verkletree.Update{verkletree.Delete(retainedDelete)},
		},
		{
			name: "replace-emptied-stem",
			entries: []verkletree.Entry{
				{Key: replaced, Value: publicValue(1)},
			},
			updates: []verkletree.Update{
				verkletree.Delete(replaced),
				verkletree.Set(replacement, publicValue(2)),
			},
		},
		{
			name: "collapse-unary-parent",
			entries: []verkletree.Entry{
				{Key: collapsed, Value: publicValue(1)},
				{Key: collapseSurvivor, Value: publicValue(2)},
			},
			updates: []verkletree.Update{verkletree.Delete(collapsed)},
		},
	}

	for _, test := range tests {
		test := test
		b.Run(test.name, func(b *testing.B) {
			snapshot, err := verkletree.NewSnapshot(
				ctx, profile, test.entries, publicSnapshotLimits(),
			)
			if err != nil {
				b.Fatalf("prepare topology snapshot: %v", err)
			}
			proof, err := proofEngine.ProveUpdates(
				ctx, snapshot, test.updates, publicTopologyProofGenerationLimits(),
			)
			if err != nil {
				b.Fatalf("prepare topology proof: %v", err)
			}
			next, _, err := snapshot.Apply(ctx, test.updates)
			if err != nil {
				b.Fatalf("prepare topology post-state: %v", err)
			}
			postRoot, err := next.Root()
			if err != nil {
				b.Fatalf("prepare topology post-root: %v", err)
			}
			witness, err := verkletree.NewWitness(
				ctx, proof, test.updates, postRoot, publicWitnessLimits(),
			)
			if err != nil {
				b.Fatalf("prepare topology witness: %v", err)
			}
			encoded, err := witness.Bytes(ctx, publicTopologyWitnessEncodingLimits())
			if err != nil {
				b.Fatalf("encode topology witness: %v", err)
			}

			b.Run("generate-proof", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if _, err := proofEngine.ProveUpdates(
						ctx, snapshot, test.updates,
						publicTopologyProofGenerationLimits(),
					); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(len(encoded)), "witness-bytes/op")
				benchmarkPublicReportThroughput(b)
			})
			b.Run("verify-and-apply", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if _, err := statelessEngine.Apply(
						ctx, witness,
						publicTopologyProofVerificationLimits(),
						publicTopologyStatelessUpdateLimits(),
					); err != nil {
						b.Fatal(err)
					}
				}
				b.ReportMetric(float64(len(encoded)), "witness-bytes/op")
				benchmarkPublicReportThroughput(b)
			})
		})
	}
}

func benchmarkPublicEntries() []verkletree.Entry {
	entries := make([]verkletree.Entry, 32)
	for index := range entries {
		entries[index] = verkletree.Entry{
			Key:   publicKey(byte(index/4), byte(index%4)),
			Value: publicValue(byte(index + 1)),
		}
	}

	return entries
}

func benchmarkPublicState(b testing.TB) verkletree.Snapshot {
	b.Helper()

	snapshot, err := verkletree.NewSnapshot(
		context.Background(),
		verkletree.BandersnatchIPA256V0(),
		benchmarkPublicEntries(),
		publicSnapshotLimits(),
	)
	if err != nil {
		b.Fatalf("prepare public state: %v", err)
	}

	return snapshot
}

func benchmarkPublicBatch() []verkletree.Update {
	updates := make([]verkletree.Update, 0, 16)
	for index := byte(0); index < 4; index++ {
		updates = append(
			updates,
			verkletree.Set(publicKey(index, 0), publicValue(0xb0+index)),
			verkletree.Delete(publicKey(index, 1)),
			verkletree.Set(publicKey(index, 0x80+index), publicValue(0xc0+index)),
			verkletree.Set(publicKey(0x80+index, index), publicValue(0xd0+index)),
		)
	}

	return updates
}

func benchmarkPublicApply(
	b *testing.B,
	snapshot verkletree.Snapshot,
	updates []verkletree.Update,
) {
	b.Helper()
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := snapshot.Apply(context.Background(), updates); err != nil {
			b.Fatal(err)
		}
	}
	benchmarkPublicReportThroughput(b)
}

func benchmarkPublicProof(
	b testing.TB,
	engine verkletree.ProofEngine,
	snapshot verkletree.Snapshot,
	keys []verkletree.Key,
) verkletree.Proof {
	b.Helper()

	proof, err := engine.Prove(
		context.Background(), snapshot, keys, publicProofGenerationLimits(),
	)
	if err != nil {
		b.Fatalf("prepare proof: %v", err)
	}

	return proof
}

func benchmarkPublicProve(
	b *testing.B,
	engine verkletree.ProofEngine,
	snapshot verkletree.Snapshot,
	keys []verkletree.Key,
) {
	b.Helper()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := engine.Prove(
			context.Background(), snapshot, keys, publicProofGenerationLimits(),
		); err != nil {
			b.Fatal(err)
		}
	}
	benchmarkPublicReportThroughput(b)
}

func benchmarkPublicVerify(
	b *testing.B,
	engine verkletree.ProofEngine,
	proof verkletree.Proof,
	proofBytes int,
) {
	b.Helper()
	b.ReportAllocs()
	for b.Loop() {
		if err := engine.Verify(
			context.Background(), proof, publicProofVerificationLimits(),
		); err != nil {
			b.Fatal(err)
		}
	}
	if proofBytes > 0 {
		b.ReportMetric(float64(proofBytes), "proof-bytes/op")
	}
	benchmarkPublicReportThroughput(b)
}

func benchmarkPublicReportThroughput(b *testing.B) {
	b.Helper()
	if elapsed := b.Elapsed(); elapsed > 0 {
		b.ReportMetric(float64(b.N)/elapsed.Seconds(), "ops/s")
	}
}
