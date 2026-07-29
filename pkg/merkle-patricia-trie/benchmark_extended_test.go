package mpt_test

import (
	"context"
	"sync/atomic"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
	"github.com/faustbrian/golib/pkg/merkle-patricia-trie/memory"
)

func BenchmarkComparableRawGetOwned(b *testing.B) {
	trie := benchmarkPopulatedTrie(b, 1024)
	ctx := context.Background()
	key := benchmarkKey(512)
	b.ResetTimer()
	for b.Loop() {
		value, err := trie.Get(ctx, key)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkBytes = value
	}
}

func BenchmarkParallelGet(b *testing.B) {
	trie := benchmarkPopulatedTrie(b, 1024)
	ctx := context.Background()
	key := benchmarkKey(512)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var value []byte
		for pb.Next() {
			var err error
			value, err = trie.Get(ctx, key)
			if err != nil {
				b.Fatal(err)
			}
		}
		_ = value
	})
}

func BenchmarkOrdinaryConstruction(b *testing.B) {
	keys, values := benchmarkCorpus(1024)
	ctx := context.Background()
	limits := mpt.DefaultLimits()
	b.ResetTimer()
	for b.Loop() {
		trie, err := mpt.NewRawTrie(limits)
		if err != nil {
			b.Fatal(err)
		}
		for index := range keys {
			trie, err = trie.Update(ctx, keys[index], values[index])
			if err != nil {
				b.Fatal(err)
			}
		}
		benchmarkRoot, err = trie.Root()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStoredGet(b *testing.B) {
	ctx := context.Background()
	store := memory.New()
	trie := benchmarkPopulatedTrie(b, 1024)
	committed, err := trie.Commit(ctx, store)
	if err != nil {
		b.Fatal(err)
	}
	root, err := committed.Root()
	if err != nil {
		b.Fatal(err)
	}
	key := benchmarkKey(512)

	b.Run("LoadedSnapshot", func(b *testing.B) {
		reader := &benchmarkCountingReader{reader: store}
		loaded, loadErr := mpt.LoadRawTrie(root, reader, mpt.DefaultLimits())
		if loadErr != nil {
			b.Fatal(loadErr)
		}
		b.ResetTimer()
		for b.Loop() {
			benchmarkBytes, loadErr = loaded.Get(ctx, key)
			if loadErr != nil {
				b.Fatal(loadErr)
			}
		}
		b.ReportMetric(
			float64(reader.reads.Load())/float64(b.N),
			"reads/op",
		)
	})

	b.Run("ReloadedSnapshot", func(b *testing.B) {
		reader := &benchmarkCountingReader{reader: store}
		b.ResetTimer()
		for b.Loop() {
			loaded, loadErr := mpt.LoadRawTrie(
				root,
				reader,
				mpt.DefaultLimits(),
			)
			if loadErr != nil {
				b.Fatal(loadErr)
			}
			benchmarkBytes, loadErr = loaded.Get(ctx, key)
			if loadErr != nil {
				b.Fatal(loadErr)
			}
		}
		b.ReportMetric(
			float64(reader.reads.Load())/float64(b.N),
			"reads/op",
		)
	})
}

func BenchmarkEIP1186ProofSetVerification(b *testing.B) {
	ctx := context.Background()
	limits := mpt.DefaultLimits()
	storage, err := mpt.NewStorageTrie(limits)
	if err != nil {
		b.Fatal(err)
	}
	const slotCount = 16
	var slots [slotCount][32]byte
	values := make([][]byte, slotCount)
	for index := range slots {
		slots[index][31] = byte(index + 1)
		var word [32]byte
		word[31] = byte(index + 1)
		storage, err = storage.UpdateSlot(ctx, slots[index], word)
		if err != nil {
			b.Fatal(err)
		}
		values[index] = []byte{byte(index + 1)}
	}
	storageRoot, err := storage.Root()
	if err != nil {
		b.Fatal(err)
	}
	var address [20]byte
	address[19] = 0xaa
	var balance [32]byte
	balance[31] = 42
	accountValue, err := mpt.NewAccountValue(
		1,
		balance,
		storageRoot,
		mpt.EmptyCodeHash(),
		limits,
	)
	if err != nil {
		b.Fatal(err)
	}
	state, err := mpt.NewStateTrie(limits)
	if err != nil {
		b.Fatal(err)
	}
	state, err = state.UpdateAccount(ctx, address, accountValue)
	if err != nil {
		b.Fatal(err)
	}
	stateRoot, err := state.Root()
	if err != nil {
		b.Fatal(err)
	}
	accountProof, err := state.ProveAccount(ctx, address)
	if err != nil {
		b.Fatal(err)
	}
	storageProofs := make([]mpt.Proof, slotCount)
	proofNodes, proofBytes := benchmarkProofSize(accountProof)
	for index := range storageProofs {
		storageProofs[index], err = storage.ProveSlot(ctx, slots[index])
		if err != nil {
			b.Fatal(err)
		}
		nodes, bytes := benchmarkProofSize(storageProofs[index])
		proofNodes += nodes
		proofBytes += bytes
	}
	encodedAccount := accountValue.Bytes()

	b.ResetTimer()
	for b.Loop() {
		account, verifyErr := mpt.VerifyAccountProof(
			ctx,
			stateRoot,
			address,
			encodedAccount,
			accountProof,
			limits,
		)
		if verifyErr != nil {
			b.Fatal(verifyErr)
		}
		for index := range storageProofs {
			if verifyErr = mpt.VerifyStorageProof(
				ctx,
				account,
				slots[index],
				values[index],
				storageProofs[index],
				limits,
			); verifyErr != nil {
				b.Fatal(verifyErr)
			}
		}
	}
	b.ReportMetric(float64(proofNodes), "proof-nodes")
	b.ReportMetric(float64(proofBytes), "proof-B")
}

func BenchmarkMalformedProofRejection(b *testing.B) {
	trie := benchmarkPopulatedTrie(b, 1024)
	ctx := context.Background()
	key := benchmarkKey(512)
	root, err := trie.Root()
	if err != nil {
		b.Fatal(err)
	}
	proof, err := trie.Prove(ctx, key)
	if err != nil {
		b.Fatal(err)
	}
	nodes := proof.Nodes()
	nodes[0][len(nodes[0])-1] ^= 1
	malformed, err := mpt.ProofFromNodes(nodes, mpt.DefaultLimits())
	if err != nil {
		b.Fatal(err)
	}
	value := benchmarkValue(512)
	b.ResetTimer()
	for b.Loop() {
		if verifyErr := mpt.VerifyRawMembership(
			ctx,
			root,
			key,
			value,
			malformed,
			mpt.DefaultLimits(),
		); verifyErr == nil {
			b.Fatal("mutated proof was accepted")
		}
	}
}

func BenchmarkCorruptStoredNodeRejection(b *testing.B) {
	ctx := context.Background()
	store := memory.New()
	trie := benchmarkPopulatedTrie(b, 1024)
	committed, err := trie.Commit(ctx, store)
	if err != nil {
		b.Fatal(err)
	}
	root, err := committed.Root()
	if err != nil {
		b.Fatal(err)
	}
	reader := &benchmarkCorruptingReader{reader: store, target: root}
	key := benchmarkKey(512)
	b.ResetTimer()
	for b.Loop() {
		loaded, loadErr := mpt.LoadRawTrie(
			root,
			reader,
			mpt.DefaultLimits(),
		)
		if loadErr != nil {
			b.Fatal(loadErr)
		}
		if _, loadErr = loaded.Get(ctx, key); loadErr == nil {
			b.Fatal("corrupt stored node was accepted")
		}
	}
	b.ReportMetric(
		float64(reader.reads.Load())/float64(b.N),
		"reads/op",
	)
}

func BenchmarkStateRebuild(b *testing.B) {
	ctx := context.Background()
	limits := mpt.DefaultLimits()
	state, err := mpt.NewStateTrie(limits)
	if err != nil {
		b.Fatal(err)
	}
	for index := range 256 {
		var address [20]byte
		address[16] = byte(index >> 24)
		address[17] = byte(index >> 16)
		address[18] = byte(index >> 8)
		address[19] = byte(index)
		var balance [32]byte
		balance[30] = byte(index >> 8)
		balance[31] = byte(index + 1)
		value, valueErr := mpt.NewAccountValue(
			uint64(index),
			balance,
			mpt.EmptyRoot(),
			mpt.EmptyCodeHash(),
			limits,
		)
		if valueErr != nil {
			b.Fatal(valueErr)
		}
		state, err = state.UpdateAccount(ctx, address, value)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for b.Loop() {
		benchmarkState, err = state.Rebuild(ctx)
		if err != nil {
			b.Fatal(err)
		}
	}
}

type benchmarkCountingReader struct {
	reader mpt.NodeReader
	reads  atomic.Uint64
}

func (reader *benchmarkCountingReader) GetNode(
	ctx context.Context,
	hash mpt.Root,
) ([]byte, error) {
	reader.reads.Add(1)
	return reader.reader.GetNode(ctx, hash)
}

type benchmarkCorruptingReader struct {
	reader mpt.NodeReader
	target mpt.Root
	reads  atomic.Uint64
}

func (reader *benchmarkCorruptingReader) GetNode(
	ctx context.Context,
	hash mpt.Root,
) ([]byte, error) {
	reader.reads.Add(1)
	encoded, err := reader.reader.GetNode(ctx, hash)
	if err != nil || hash != reader.target || len(encoded) == 0 {
		return encoded, err
	}
	encoded[0] ^= 1
	return encoded, nil
}

func benchmarkCorpus(size int) ([][]byte, [][]byte) {
	keys := make([][]byte, size)
	values := make([][]byte, size)
	for index := range keys {
		keys[index] = benchmarkKey(index)
		values[index] = benchmarkValue(index)
	}
	return keys, values
}

func benchmarkProofSize(proof mpt.Proof) (int, int) {
	nodes := proof.Nodes()
	bytes := 0
	for _, encoded := range nodes {
		bytes += len(encoded)
	}
	return len(nodes), bytes
}

func benchmarkMultiProofSize(proof mpt.MultiProof) (int, int) {
	nodes := proof.Nodes()
	bytes := 0
	for _, encoded := range nodes {
		bytes += len(encoded)
	}
	return len(nodes), bytes
}

func benchmarkRangeProofSize(proof mpt.RangeProof) (int, int) {
	nodes := proof.Nodes()
	bytes := 0
	for _, encoded := range nodes {
		bytes += len(encoded)
	}
	return len(nodes), bytes
}
