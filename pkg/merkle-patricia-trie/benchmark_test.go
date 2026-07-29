package mpt_test

import (
	"context"
	"encoding/binary"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/memory"
)

var (
	benchmarkBytes []byte
	benchmarkRoot  mpt.Root
	benchmarkTrie  mpt.RawTrie
	benchmarkPrune mpt.PruneResult
)

func BenchmarkGetEmpty(b *testing.B) {
	trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	key := []byte("missing")
	for b.Loop() {
		benchmarkBytes, _ = trie.Get(ctx, key)
	}
}

func BenchmarkGetPopulated(b *testing.B) {
	trie := benchmarkPopulatedTrie(b, 1024)
	ctx := context.Background()
	key := benchmarkKey(512)
	b.ResetTimer()
	for b.Loop() {
		var err error
		benchmarkBytes, err = trie.Get(ctx, key)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUpdate(b *testing.B) {
	trie := benchmarkPopulatedTrie(b, 1024)
	ctx := context.Background()
	key := []byte("new-key")
	value := []byte("new-value")
	b.ResetTimer()
	for b.Loop() {
		var err error
		benchmarkTrie, err = trie.Update(ctx, key, value)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReplace(b *testing.B) {
	trie := benchmarkPopulatedTrie(b, 1024)
	ctx := context.Background()
	key := benchmarkKey(512)
	value := []byte("replacement")
	b.ResetTimer()
	for b.Loop() {
		var err error
		benchmarkTrie, err = trie.Update(ctx, key, value)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDelete(b *testing.B) {
	trie := benchmarkPopulatedTrie(b, 1024)
	ctx := context.Background()
	key := benchmarkKey(512)
	b.ResetTimer()
	for b.Loop() {
		var err error
		benchmarkTrie, err = trie.Delete(ctx, key)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAtomicBatch(b *testing.B) {
	trie := benchmarkPopulatedTrie(b, 1024)
	ctx := context.Background()
	mutations := make([]mpt.Mutation, 16)
	for index := range mutations {
		mutations[index] = mpt.Put(benchmarkKey(2048+index), []byte{byte(index + 1)})
	}
	b.ResetTimer()
	for b.Loop() {
		var err error
		benchmarkTrie, err = trie.ApplyBatch(ctx, mutations)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRoot(b *testing.B) {
	trie := benchmarkPopulatedTrie(b, 1024)
	b.ResetTimer()
	for b.Loop() {
		var err error
		benchmarkRoot, err = trie.Root()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTransactionRoot(b *testing.B) {
	limits := mpt.DefaultLimits()
	values := make([]mpt.EncodedTransactionValue, 256)
	for index := range values {
		var err error
		values[index], err = mpt.TypedTransactionValue(
			mpt.LondonProfile, 2, []byte{0xc1, byte(index%0x7f + 1)}, limits,
		)
		if err != nil {
			b.Fatal(err)
		}
	}
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		var err error
		benchmarkRoot, err = mpt.TransactionRoot(ctx, values, limits)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReceiptRoot(b *testing.B) {
	limits := mpt.DefaultLimits()
	transactions := make([]mpt.EncodedTransactionValue, 256)
	receipts := make([]mpt.EncodedReceiptValue, len(transactions))
	for index := range transactions {
		payload := []byte{0xc1, byte(index%0x7f + 1)}
		var err error
		transactions[index], err = mpt.TypedTransactionValue(
			mpt.LondonProfile, 2, payload, limits,
		)
		if err != nil {
			b.Fatal(err)
		}
		receipts[index], err = mpt.TypedReceiptValue(
			mpt.LondonProfile, 2, payload, limits,
		)
		if err != nil {
			b.Fatal(err)
		}
	}
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		var err error
		benchmarkRoot, err = mpt.ReceiptRoot(
			ctx, transactions, receipts, limits,
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCommit(b *testing.B) {
	trie := benchmarkPopulatedTrie(b, 1024)
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		store := memory.New()
		committed, err := trie.Commit(ctx, store)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkRoot, err = committed.Root()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkProofGeneration(b *testing.B) {
	trie := benchmarkPopulatedTrie(b, 1024)
	ctx := context.Background()
	key := benchmarkKey(512)
	b.ResetTimer()
	for b.Loop() {
		proof, err := trie.Prove(ctx, key)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkBytes = proof.Nodes()[0]
	}
}

func BenchmarkProofVerification(b *testing.B) {
	trie := benchmarkPopulatedTrie(b, 1024)
	ctx := context.Background()
	key := benchmarkKey(512)
	value := benchmarkValue(512)
	root, err := trie.Root()
	if err != nil {
		b.Fatal(err)
	}
	proof, err := trie.Prove(ctx, key)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		if err := mpt.VerifyRawMembership(
			ctx, root, key, value, proof, mpt.DefaultLimits(),
		); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMultiProofGeneration(b *testing.B) {
	trie := benchmarkPopulatedTrie(b, 1024)
	ctx := context.Background()
	keys := make([][]byte, 32)
	for index := range keys {
		keys[index] = benchmarkKey(index * 31)
	}
	b.ResetTimer()
	for b.Loop() {
		proof, err := trie.ProveMany(ctx, keys)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkBytes = proof.Nodes()[0]
	}
}

func BenchmarkMultiProofVerification(b *testing.B) {
	trie := benchmarkPopulatedTrie(b, 1024)
	ctx := context.Background()
	keys := make([][]byte, 32)
	claims := make([]mpt.ProofClaim, len(keys))
	for index := range keys {
		keys[index] = benchmarkKey(index * 31)
		claims[index] = mpt.MembershipClaim(
			keys[index], benchmarkValue(index*31),
		)
	}
	root, err := trie.Root()
	if err != nil {
		b.Fatal(err)
	}
	proof, err := trie.ProveMany(ctx, keys)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		if err := mpt.VerifyRawMultiProof(
			ctx, root, claims, proof, mpt.DefaultLimits(),
		); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRangeProofGeneration(b *testing.B) {
	trie := benchmarkPopulatedTrie(b, 1024)
	ctx := context.Background()
	start := benchmarkKey(400)
	end := benchmarkKey(432)
	b.ResetTimer()
	for b.Loop() {
		proof, _, err := trie.ProveRange(ctx, start, end)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkBytes = proof.Nodes()[0]
	}
}

func BenchmarkRangeProofVerification(b *testing.B) {
	trie := benchmarkPopulatedTrie(b, 1024)
	ctx := context.Background()
	start := benchmarkKey(400)
	end := benchmarkKey(432)
	root, err := trie.Root()
	if err != nil {
		b.Fatal(err)
	}
	proof, items, err := trie.ProveRange(ctx, start, end)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		if err := mpt.VerifyRawRange(
			ctx,
			root,
			start,
			end,
			items,
			proof,
			mpt.DefaultLimits(),
		); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFullIteration(b *testing.B) {
	trie := benchmarkPopulatedTrie(b, 1024)
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		if err := trie.Iterate(
			ctx,
			mpt.IterationOptions{},
			func(entry mpt.Entry) error {
				benchmarkBytes = entry.Value()
				return nil
			},
		); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPrefixIteration(b *testing.B) {
	trie := benchmarkPopulatedTrie(b, 1024)
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		if err := trie.Iterate(
			ctx,
			mpt.IterationOptions{Prefix: []byte{0, 0}},
			func(entry mpt.Entry) error {
				benchmarkBytes = entry.Value()
				return nil
			},
		); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRebuild(b *testing.B) {
	trie := benchmarkPopulatedTrie(b, 1024)
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		var err error
		benchmarkTrie, err = trie.Rebuild(ctx)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMissingNodeRecovery(b *testing.B) {
	store := newTestNodeStore()
	trie := benchmarkPopulatedTrie(b, 1024)
	committed, err := trie.Commit(context.Background(), store)
	if err != nil {
		b.Fatal(err)
	}
	root, err := committed.Root()
	if err != nil {
		b.Fatal(err)
	}
	encoded := append([]byte(nil), store.nodes[root]...)
	loaded, err := mpt.LoadRawTrie(root, store, mpt.DefaultLimits())
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		benchmarkTrie, err = loaded.RecoverNode(ctx, root, encoded)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPrune(b *testing.B) {
	ctx := context.Background()
	for b.Loop() {
		b.StopTimer()
		store := memory.New()
		trie := benchmarkPopulatedTrie(b, 256)
		var err error
		trie, err = trie.Commit(ctx, store)
		if err != nil {
			b.Fatal(err)
		}
		for index := range 32 {
			trie, err = trie.Update(
				ctx, benchmarkKey(index), benchmarkValue(index+1024),
			)
			if err != nil {
				b.Fatal(err)
			}
		}
		if _, err = trie.Commit(ctx, store); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		benchmarkPrune, err = store.Prune(
			ctx, mpt.DefaultReachabilityLimits(),
		)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSortedConstruction(b *testing.B) {
	keys := make([][]byte, 1024)
	values := make([][]byte, len(keys))
	for index := range keys {
		keys[index] = benchmarkKey(index)
		values[index] = benchmarkValue(index)
	}
	ctx := context.Background()
	b.ResetTimer()
	for b.Loop() {
		builder, err := mpt.NewSortedBuilder(mpt.DefaultLimits())
		if err != nil {
			b.Fatal(err)
		}
		for index := range keys {
			if err := builder.Add(ctx, keys[index], values[index]); err != nil {
				b.Fatal(err)
			}
		}
		benchmarkRoot, err = builder.Finalize(ctx)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkPopulatedTrie(tb testing.TB, size int) mpt.RawTrie {
	tb.Helper()
	trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		tb.Fatal(err)
	}
	ctx := context.Background()
	for index := range size {
		trie, err = trie.Update(ctx, benchmarkKey(index), benchmarkValue(index))
		if err != nil {
			tb.Fatal(err)
		}
	}
	return trie
}

func benchmarkKey(index int) []byte {
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, uint64(index))
	return key
}

func benchmarkValue(index int) []byte {
	value := make([]byte, 32)
	binary.BigEndian.PutUint64(value[24:], uint64(index+1))
	return value
}
