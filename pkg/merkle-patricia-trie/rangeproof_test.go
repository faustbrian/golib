package mpt_test

import (
	"context"
	"errors"
	"slices"
	"sort"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
)

func TestRawRangeProofEstablishesEveryLeafInExplicitInterval(t *testing.T) {
	t.Parallel()

	trie := mustRawTrie(t, map[string]string{
		"":   "empty",
		"a":  "before",
		"aa": "first",
		"ab": "second",
		"b":  "third",
		"c":  "after",
	})
	proof, items, err := trie.ProveRange(
		context.Background(), []byte("aa"), []byte("c"),
	)
	if err != nil {
		t.Fatalf("ProveRange() error = %v", err)
	}
	got := make([]string, len(items))
	for index, item := range items {
		got[index] = string(item.Key()) + "=" + string(item.Value())
	}
	if !slices.Equal(got, []string{"aa=first", "ab=second", "b=third"}) {
		t.Fatalf("ProveRange() items = %q", got)
	}

	root, err := trie.Root()
	if err != nil {
		t.Fatalf("Root() error = %v", err)
	}
	if err := mpt.VerifyRawRange(
		context.Background(),
		root,
		[]byte("aa"),
		[]byte("c"),
		items,
		proof,
		mpt.DefaultLimits(),
	); err != nil {
		t.Fatalf("VerifyRawRange() error = %v", err)
	}
}

func TestRangeProofIncludesPrefixKeysAndUnboundedEndpoints(t *testing.T) {
	t.Parallel()

	trie := mustRawTrie(t, map[string]string{
		"":   "empty",
		"a":  "a",
		"aa": "aa",
		"ab": "ab",
		"b":  "b",
	})
	proof, items, err := trie.ProveRange(
		context.Background(), []byte("a"), []byte("b"),
	)
	if err != nil {
		t.Fatalf("ProveRange(prefix interval) error = %v", err)
	}
	assertRangeItems(t, items, []string{"a=a", "aa=aa", "ab=ab"})
	if err := mpt.VerifyRawRange(
		context.Background(),
		mustTrieRoot(t, trie),
		[]byte("a"),
		[]byte("b"),
		items,
		proof,
		mpt.DefaultLimits(),
	); err != nil {
		t.Fatalf("VerifyRawRange(prefix interval) error = %v", err)
	}

	proof, items, err = trie.ProveRange(
		context.Background(), []byte("b"), nil,
	)
	if err != nil {
		t.Fatalf("ProveRange(unbounded interval) error = %v", err)
	}
	assertRangeItems(t, items, []string{"b=b"})
	if err := mpt.VerifyRawRange(
		context.Background(),
		mustTrieRoot(t, trie),
		[]byte("b"),
		nil,
		items,
		proof,
		mpt.DefaultLimits(),
	); err != nil {
		t.Fatalf("VerifyRawRange(unbounded interval) error = %v", err)
	}
}

func TestRangeProofMatchesEverySmallByteInterval(t *testing.T) {
	t.Parallel()

	trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	for key := range 16 {
		trie, err = trie.Update(
			context.Background(),
			[]byte{byte(key)},
			[]byte{byte(key + 1)},
		)
		if err != nil {
			t.Fatalf("Update(%d) error = %v", key, err)
		}
	}
	root := mustTrieRoot(t, trie)
	for start := range 17 {
		for end := start + 1; end <= 17; end++ {
			proof, items, proveErr := trie.ProveRange(
				context.Background(), []byte{byte(start)}, []byte{byte(end)},
			)
			if proveErr != nil {
				t.Fatalf("ProveRange([%d,%d)) error = %v", start, end, proveErr)
			}
			wantCount := min(end, 16) - min(start, 16)
			if len(items) != wantCount {
				t.Fatalf(
					"ProveRange([%d,%d)) returned %d items, want %d",
					start,
					end,
					len(items),
					wantCount,
				)
			}
			for index, item := range items {
				wantKey := byte(start + index)
				if !slices.Equal(item.Key(), []byte{wantKey}) ||
					!slices.Equal(item.Value(), []byte{wantKey + 1}) {
					t.Fatalf(
						"ProveRange([%d,%d)) item %d = (%x,%x)",
						start,
						end,
						index,
						item.Key(),
						item.Value(),
					)
				}
			}
			if verifyErr := mpt.VerifyRawRange(
				context.Background(),
				root,
				[]byte{byte(start)},
				[]byte{byte(end)},
				items,
				proof,
				mpt.DefaultLimits(),
			); verifyErr != nil {
				t.Fatalf(
					"VerifyRawRange([%d,%d)) error = %v",
					start,
					end,
					verifyErr,
				)
			}
		}
	}
}

func TestRangeProofRejectsAlteredLeavesAndProofNodes(t *testing.T) {
	t.Parallel()

	long := "a value long enough to persist each selected child independently"
	trie := mustRawTrie(t, map[string]string{
		"aa": long + " aa",
		"ab": long + " ab",
		"ac": long + " ac",
		"ba": long + " ba",
	})
	proof, items, err := trie.ProveRange(
		context.Background(), []byte("aa"), []byte("b"),
	)
	if err != nil {
		t.Fatalf("ProveRange() error = %v", err)
	}
	root := mustTrieRoot(t, trie)
	verify := func(items []mpt.RangeItem, nodes [][]byte) error {
		t.Helper()
		transport, transportErr := mpt.RangeProofFromNodes(
			nodes, mpt.DefaultLimits(),
		)
		if transportErr != nil {
			return transportErr
		}
		return mpt.VerifyRawRange(
			context.Background(),
			root,
			[]byte("aa"),
			[]byte("b"),
			items,
			transport,
			mpt.DefaultLimits(),
		)
	}

	altered := append([]mpt.RangeItem(nil), items...)
	altered[1] = mpt.NewRangeItem(altered[1].Key(), []byte("wrong"))
	if err := verify(altered, proof.Nodes()); !errors.Is(err, mpt.ErrFailedProof) {
		t.Fatalf("altered item error = %v, want ErrFailedProof", err)
	}
	if err := verify(items[:len(items)-1], proof.Nodes()); !errors.Is(
		err, mpt.ErrFailedProof,
	) {
		t.Fatalf("missing item error = %v, want ErrFailedProof", err)
	}

	nodes := proof.Nodes()
	if len(nodes) < 2 {
		t.Fatalf("proof has %d nodes, want multiple hashed nodes", len(nodes))
	}
	if err := verify(items, nodes[:len(nodes)-1]); !errors.Is(
		err, mpt.ErrIncompleteProof,
	) {
		t.Fatalf("missing node error = %v, want ErrIncompleteProof", err)
	}
	surplus := append(proof.Nodes(), proof.Nodes()[0])
	if err := verify(items, surplus); !errors.Is(err, mpt.ErrMalformedProof) {
		t.Fatalf("surplus node error = %v, want ErrMalformedProof", err)
	}
	otherTrie := mustRawTrie(t, map[string]string{"z": long + " z"})
	otherProof, _, err := otherTrie.ProveRange(
		context.Background(), nil, nil,
	)
	if err != nil {
		t.Fatalf("other ProveRange() error = %v", err)
	}
	uniqueSurplus := append(proof.Nodes(), otherProof.Nodes()[0])
	if err := verify(items, uniqueSurplus); !errors.Is(
		err, mpt.ErrMalformedProof,
	) {
		t.Fatalf("unique surplus error = %v, want ErrMalformedProof", err)
	}
	mutated := proof.Nodes()
	mutated[0][0] ^= 0x01
	if err := verify(items, mutated); !errors.Is(err, mpt.ErrMalformedProof) {
		t.Fatalf("mutated root error = %v, want ErrMalformedProof", err)
	}
}

func TestRangeProofOwnsTransportAndClaimBytes(t *testing.T) {
	t.Parallel()

	key := []byte("a")
	value := []byte("value")
	item := mpt.NewRangeItem(key, value)
	key[0] = 'x'
	value[0] = 'x'
	if string(item.Key()) != "a" || string(item.Value()) != "value" {
		t.Fatalf("RangeItem aliased constructor input")
	}
	item.Key()[0] = 'x'
	item.Value()[0] = 'x'
	if string(item.Key()) != "a" || string(item.Value()) != "value" {
		t.Fatalf("RangeItem getters exposed internal bytes")
	}

	trie := mustRawTrie(t, map[string]string{"a": "value"})
	proof, _, err := trie.ProveRange(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("ProveRange() error = %v", err)
	}
	nodes := proof.Nodes()
	transport, err := mpt.RangeProofFromNodes(nodes, mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("RangeProofFromNodes() error = %v", err)
	}
	nodes[0][0] ^= 0x01
	returned := transport.Nodes()
	returned[0][0] ^= 0x01
	if err := mpt.VerifyRawRange(
		context.Background(),
		mustTrieRoot(t, trie),
		nil,
		nil,
		[]mpt.RangeItem{item},
		transport,
		mpt.DefaultLimits(),
	); err != nil {
		t.Fatalf("owned VerifyRawRange() error = %v", err)
	}
}

func TestSecureHashedRangeProofUsesExplicitTransformedKeys(t *testing.T) {
	t.Parallel()

	trie, err := mpt.NewSecureTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewSecureTrie() error = %v", err)
	}
	for _, key := range []string{"alpha", "beta", "gamma"} {
		trie, err = trie.Update(
			context.Background(), []byte(key), []byte("value-"+key),
		)
		if err != nil {
			t.Fatalf("Update(%q) error = %v", key, err)
		}
	}
	hashed := [][]byte{
		legacyKeccakForTest([]byte("alpha")),
		legacyKeccakForTest([]byte("beta")),
		legacyKeccakForTest([]byte("gamma")),
	}
	sort.Slice(hashed, func(left, right int) bool {
		return slices.Compare(hashed[left], hashed[right]) < 0
	})

	proof, items, err := trie.ProveHashedRange(
		context.Background(), hashed[0], hashed[2],
	)
	if err != nil {
		t.Fatalf("ProveHashedRange() error = %v", err)
	}
	if len(items) != 2 ||
		!slices.Equal(items[0].Key(), hashed[0]) ||
		!slices.Equal(items[1].Key(), hashed[1]) {
		t.Fatalf("ProveHashedRange() keys = %v", items)
	}
	root, err := trie.Root()
	if err != nil {
		t.Fatalf("Root() error = %v", err)
	}
	if err := mpt.VerifySecureHashedRange(
		context.Background(),
		root,
		hashed[0],
		hashed[2],
		items,
		proof,
		mpt.DefaultLimits(),
	); err != nil {
		t.Fatalf("VerifySecureHashedRange() error = %v", err)
	}
	if _, _, err := trie.ProveHashedRange(
		context.Background(), []byte("raw"), nil,
	); !errors.Is(err, mpt.ErrInvalidProofClaim) {
		t.Fatalf("ProveHashedRange(raw endpoint) error = %v", err)
	}
	if _, _, err := trie.ProveHashedRange(
		context.Background(), nil, []byte("raw"),
	); !errors.Is(err, mpt.ErrInvalidProofClaim) {
		t.Fatalf("ProveHashedRange(raw end) error = %v", err)
	}
}

func TestRangeProofEmptyTrieAndInputValidation(t *testing.T) {
	t.Parallel()

	trie, err := mpt.NewRawTrie(mpt.DefaultLimits())
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	proof, items, err := trie.ProveRange(
		context.Background(), []byte("a"), []byte("b"),
	)
	if err != nil {
		t.Fatalf("empty ProveRange() error = %v", err)
	}
	if len(proof.Nodes()) != 0 || len(items) != 0 {
		t.Fatalf("empty ProveRange() = (%x, %v)", proof.Nodes(), items)
	}
	if err := mpt.VerifyRawRange(
		context.Background(),
		mpt.EmptyRoot(),
		[]byte("a"),
		[]byte("b"),
		nil,
		proof,
		mpt.DefaultLimits(),
	); err != nil {
		t.Fatalf("empty VerifyRawRange() error = %v", err)
	}
	if _, _, err := trie.ProveRange(
		context.Background(), []byte("b"), []byte("a"),
	); !errors.Is(err, mpt.ErrInvalidProofClaim) {
		t.Fatalf("reversed ProveRange() error = %v", err)
	}
	if _, _, err := trie.ProveRange(
		context.Background(), []byte("a"), []byte("a"),
	); !errors.Is(err, mpt.ErrInvalidProofClaim) {
		t.Fatalf("equal ProveRange() error = %v", err)
	}
	if err := mpt.VerifyRawRange(
		context.Background(),
		mpt.EmptyRoot(),
		nil,
		nil,
		[]mpt.RangeItem{{}},
		mpt.RangeProof{},
		mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrInvalidProofClaim) {
		t.Fatalf("zero item error = %v", err)
	}
}

func TestRangeProofValidatesContextLimitsBoundsItemsAndRoots(t *testing.T) {
	t.Parallel()

	trie := mustRawTrie(t, map[string]string{"a": "value", "b": "other"})
	proof, items, err := trie.ProveRange(
		context.Background(), []byte("a"), []byte("c"),
	)
	if err != nil {
		t.Fatalf("ProveRange() error = %v", err)
	}
	root := mustTrieRoot(t, trie)
	verify := func(
		ctx context.Context,
		verifyRoot mpt.Root,
		start, end []byte,
		verifyItems []mpt.RangeItem,
		verifyProof mpt.RangeProof,
		limits mpt.Limits,
	) error {
		t.Helper()
		return mpt.VerifyRawRange(
			ctx,
			verifyRoot,
			start,
			end,
			verifyItems,
			verifyProof,
			limits,
		)
	}

	var nilContext context.Context
	if _, _, err := trie.ProveRange(nilContext, nil, nil); !errors.Is(
		err, mpt.ErrInvalidContext,
	) {
		t.Fatalf("ProveRange(nil context) error = %v", err)
	}
	if err := verify(
		nilContext, root, nil, nil, items, proof, mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrInvalidContext) {
		t.Fatalf("VerifyRawRange(nil context) error = %v", err)
	}
	var zero mpt.RawTrie
	if _, _, err := zero.ProveRange(
		context.Background(), nil, nil,
	); !errors.Is(err, mpt.ErrUninitialized) {
		t.Fatalf("zero ProveRange() error = %v", err)
	}

	invalidLimits := mpt.DefaultLimits()
	invalidLimits.MaxProofKeys = 0
	if err := verify(
		context.Background(), root, nil, nil, items, proof, invalidLimits,
	); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("invalid limits error = %v", err)
	}
	if _, err := mpt.RangeProofFromNodes(proof.Nodes(), invalidLimits); !errors.Is(
		err, mpt.ErrResourceLimit,
	) {
		t.Fatalf("RangeProofFromNodes(invalid limits) error = %v", err)
	}
	small := mpt.DefaultLimits()
	small.MaxKeyBytes = 1
	if err := verify(
		context.Background(),
		root,
		[]byte("aa"),
		nil,
		nil,
		proof,
		small,
	); !errors.Is(err, mpt.ErrInvalidKey) {
		t.Fatalf("oversized endpoint error = %v", err)
	}

	if err := verify(
		context.Background(),
		root,
		[]byte("c"),
		[]byte("a"),
		items,
		proof,
		mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrInvalidProofClaim) {
		t.Fatalf("reversed range error = %v", err)
	}
	if err := verify(
		context.Background(),
		root,
		[]byte("b"),
		[]byte("c"),
		items,
		proof,
		mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrInvalidProofClaim) {
		t.Fatalf("out-of-range item error = %v", err)
	}
	if err := verify(
		context.Background(),
		root,
		nil,
		nil,
		[]mpt.RangeItem{
			mpt.NewRangeItem([]byte("b"), []byte("other")),
			mpt.NewRangeItem([]byte("a"), []byte("value")),
		},
		proof,
		mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrInvalidProofClaim) {
		t.Fatalf("out-of-order item error = %v", err)
	}
	if err := verify(
		context.Background(),
		root,
		nil,
		nil,
		[]mpt.RangeItem{mpt.NewRangeItem([]byte("a"), nil)},
		proof,
		mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrInvalidProofClaim) {
		t.Fatalf("empty value error = %v", err)
	}
	if err := verify(
		context.Background(),
		root,
		nil,
		nil,
		[]mpt.RangeItem{
			mpt.NewRangeItem([]byte("a"), []byte("value")),
			mpt.NewRangeItem([]byte("a"), []byte("value")),
		},
		proof,
		mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrInvalidProofClaim) {
		t.Fatalf("duplicate item error = %v", err)
	}
	valueLimited := mpt.DefaultLimits()
	valueLimited.MaxValueBytes = 1
	if err := verify(
		context.Background(),
		root,
		nil,
		nil,
		[]mpt.RangeItem{mpt.NewRangeItem([]byte("a"), []byte("value"))},
		proof,
		valueLimited,
	); !errors.Is(err, mpt.ErrInvalidValue) {
		t.Fatalf("oversized item value error = %v", err)
	}
	keyLimited := mpt.DefaultLimits()
	keyLimited.MaxKeyBytes = 1
	if err := verify(
		context.Background(),
		root,
		nil,
		nil,
		[]mpt.RangeItem{mpt.NewRangeItem([]byte("aa"), []byte("value"))},
		proof,
		keyLimited,
	); !errors.Is(err, mpt.ErrInvalidKey) {
		t.Fatalf("oversized item key error = %v", err)
	}
	proofLimited := mpt.DefaultLimits()
	proofLimited.MaxProofBytes = 1
	if err := verify(
		context.Background(),
		root,
		nil,
		nil,
		items,
		proof,
		proofLimited,
	); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("proof byte limit error = %v", err)
	}

	tooMany := mpt.DefaultLimits()
	tooMany.MaxProofKeys = 1
	if err := verify(
		context.Background(), root, nil, nil, items, proof, tooMany,
	); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("item count error = %v", err)
	}
	if err := verify(
		context.Background(),
		mpt.EmptyRoot(),
		nil,
		nil,
		nil,
		proof,
		mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrMalformedProof) {
		t.Fatalf("empty-root surplus error = %v", err)
	}
	if err := verify(
		context.Background(),
		mpt.EmptyRoot(),
		nil,
		nil,
		items,
		mpt.RangeProof{},
		mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrFailedProof) {
		t.Fatalf("empty-root items error = %v", err)
	}
	if err := verify(
		context.Background(),
		root,
		nil,
		nil,
		items,
		mpt.RangeProof{},
		mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrIncompleteProof) {
		t.Fatalf("missing proof error = %v", err)
	}
	outsideTrie := mustRawTrie(t, map[string]string{"z": "outside"})
	outsideProof, _, err := outsideTrie.ProveRange(
		context.Background(), []byte("a"), []byte("b"),
	)
	if err != nil {
		t.Fatalf("outside ProveRange() error = %v", err)
	}
	if err := verify(
		context.Background(),
		mustTrieRoot(t, outsideTrie),
		[]byte("a"),
		[]byte("b"),
		[]mpt.RangeItem{mpt.NewRangeItem([]byte("a"), []byte("claimed"))},
		outsideProof,
		mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrFailedProof) {
		t.Fatalf("unproven extra item error = %v", err)
	}
	wrongRoot := root
	wrongRoot[0] ^= 0xff
	if err := verify(
		context.Background(),
		wrongRoot,
		nil,
		nil,
		items,
		proof,
		mpt.DefaultLimits(),
	); !errors.Is(err, mpt.ErrWrongRoot) {
		t.Fatalf("wrong root error = %v", err)
	}
}

func TestRangeProofGenerationEnforcesItemAndProofBudgets(t *testing.T) {
	t.Parallel()

	limits := mpt.DefaultLimits()
	limits.MaxProofKeys = 1
	trie, err := mpt.NewRawTrie(limits)
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	for _, key := range []string{"a", "b"} {
		trie, err = trie.Update(
			context.Background(), []byte(key), []byte("value-"+key),
		)
		if err != nil {
			t.Fatalf("Update(%q) error = %v", key, err)
		}
	}
	if _, _, err := trie.ProveRange(
		context.Background(), nil, nil,
	); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("ProveRange(item bound) error = %v", err)
	}

	nodeLimited := mpt.DefaultLimits()
	nodeLimited.MaxProofNodes = 1
	largeTrie, err := mpt.NewRawTrie(nodeLimited)
	if err != nil {
		t.Fatalf("NewRawTrie(node limited) error = %v", err)
	}
	for _, key := range []string{"aa", "ab", "ac"} {
		largeTrie, err = largeTrie.Update(
			context.Background(),
			[]byte(key),
			[]byte("a value long enough to create hashed range children "+key),
		)
		if err != nil {
			t.Fatalf("Update(%q) error = %v", key, err)
		}
	}
	if _, _, err := largeTrie.ProveRange(
		context.Background(), nil, nil,
	); !errors.Is(err, mpt.ErrResourceLimit) {
		t.Fatalf("ProveRange(node bound) error = %v", err)
	}
}

func assertRangeItems(
	t *testing.T,
	items []mpt.RangeItem,
	want []string,
) {
	t.Helper()
	got := make([]string, len(items))
	for index, item := range items {
		got[index] = string(item.Key()) + "=" + string(item.Value())
	}
	if !slices.Equal(got, want) {
		t.Fatalf("range items = %q, want %q", got, want)
	}
}
