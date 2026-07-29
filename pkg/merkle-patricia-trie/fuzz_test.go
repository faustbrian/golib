package mpt_test

import (
	"bytes"
	"context"
	"slices"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
)

func FuzzCompactPathCanonicalRoundTrip(f *testing.F) {
	f.Add([]byte{0x20})
	f.Add([]byte{0x31, 0x23})
	f.Add([]byte{0x00, 0x12})
	f.Add([]byte{0x40})

	f.Fuzz(func(t *testing.T, encoded []byte) {
		if len(encoded) > 64 {
			return
		}
		path, err := mpt.DecodeCompactPath(encoded)
		if err != nil {
			return
		}
		roundTrip, err := mpt.EncodeCompactPath(path.Nibbles(), path.Leaf())
		if err != nil {
			t.Fatalf("EncodeCompactPath(decoded) error = %v", err)
		}
		if !bytes.Equal(roundTrip, encoded) {
			t.Fatalf("round trip = %x, want %x", roundTrip, encoded)
		}
	})
}

func FuzzEnvelopeValueValidation(f *testing.F) {
	f.Add(byte(mpt.BerlinProfile), byte(1), []byte{0xc0})
	f.Add(byte(mpt.OsakaProfile), byte(4), []byte{0xc1, 0x01})
	f.Add(byte(0), byte(0), []byte{0xff})

	f.Fuzz(func(t *testing.T, profileByte, envelopeType byte, encoded []byte) {
		if len(encoded) > 4096 {
			return
		}
		limits := mpt.DefaultLimits()
		limits.MaxValueBytes = 4097
		profile := mpt.ForkProfile(profileByte)

		transaction, transactionErr := mpt.TypedTransactionValue(
			profile, envelopeType, encoded, limits,
		)
		receipt, receiptErr := mpt.TypedReceiptValue(
			profile, envelopeType, encoded, limits,
		)
		if (transactionErr == nil) != (receiptErr == nil) {
			t.Fatalf(
				"typed validation differs: transaction=%v receipt=%v",
				transactionErr,
				receiptErr,
			)
		}
		if transactionErr == nil {
			if !bytes.Equal(transaction.Bytes(), receipt.Bytes()) ||
				!bytes.Equal(transaction.Bytes()[1:], encoded) {
				t.Fatal("typed envelope bytes were not preserved")
			}
		}

		legacyTransaction, legacyTransactionErr := mpt.LegacyTransactionValue(
			encoded, limits,
		)
		legacyReceipt, legacyReceiptErr := mpt.LegacyReceiptValue(encoded, limits)
		if (legacyTransactionErr == nil) != (legacyReceiptErr == nil) {
			t.Fatalf(
				"legacy validation differs: transaction=%v receipt=%v",
				legacyTransactionErr,
				legacyReceiptErr,
			)
		}
		if legacyTransactionErr == nil &&
			(!bytes.Equal(legacyTransaction.Bytes(), encoded) ||
				!bytes.Equal(legacyReceipt.Bytes(), encoded)) {
			t.Fatal("legacy envelope bytes were not preserved")
		}
	})
}

func FuzzProofVerificationRejectsHostileInput(f *testing.F) {
	f.Add(
		make([]byte, mpt.RootBytes),
		[]byte("key"),
		[]byte("value"),
		[]byte{0xc2, 0x20, 0x01},
	)

	f.Fuzz(func(t *testing.T, rootBytes, key, value, encoded []byte) {
		if len(rootBytes) > 64 || len(key) > 64 || len(value) > 256 ||
			len(encoded) > 4096 {
			return
		}
		var root mpt.Root
		copy(root[:], rootBytes)
		limits := mpt.DefaultLimits()
		limits.MaxKeyBytes = 64
		limits.MaxValueBytes = 256
		limits.MaxProofBytes = 4096
		var nodes [][]byte
		if len(encoded) != 0 {
			nodes = [][]byte{encoded}
		}
		proof, err := mpt.ProofFromNodes(nodes, limits)
		if err != nil {
			return
		}
		ctx := context.Background()
		_ = mpt.VerifyRawMembership(ctx, root, key, value, proof, limits)
		_ = mpt.VerifyRawAbsence(ctx, root, key, proof, limits)
		_ = mpt.VerifySecureMembership(ctx, root, key, value, proof, limits)
		_ = mpt.VerifySecureAbsence(ctx, root, key, proof, limits)
	})
}

func FuzzMultiProofVerificationRejectsHostileInput(f *testing.F) {
	f.Add(
		make([]byte, mpt.RootBytes),
		[]byte("first"),
		[]byte("second"),
		[]byte("value"),
		[]byte{0xc2, 0x20, 0x01},
	)

	f.Fuzz(func(t *testing.T, rootBytes, first, second, value, encoded []byte) {
		if len(rootBytes) > 64 || len(first) > 64 || len(second) > 64 ||
			len(value) > 256 || len(encoded) > 4096 {
			return
		}
		var root mpt.Root
		copy(root[:], rootBytes)
		limits := mpt.DefaultLimits()
		limits.MaxKeyBytes = 64
		limits.MaxValueBytes = 256
		limits.MaxProofBytes = 4096
		var nodes [][]byte
		if len(encoded) != 0 {
			nodes = [][]byte{encoded}
		}
		proof, err := mpt.MultiProofFromNodes(nodes, limits)
		if err != nil {
			return
		}
		claims := []mpt.ProofClaim{
			mpt.MembershipClaim(first, value),
			mpt.AbsenceClaim(second),
		}
		ctx := context.Background()
		_ = mpt.VerifyRawMultiProof(ctx, root, claims, proof, limits)
		_ = mpt.VerifySecureMultiProof(ctx, root, claims, proof, limits)
	})
}

func FuzzRangeProofVerificationRejectsHostileInput(f *testing.F) {
	f.Add(
		make([]byte, mpt.RootBytes),
		[]byte("a"),
		[]byte("b"),
		[]byte("value"),
		[]byte{0xc2, 0x20, 0x01},
	)

	f.Fuzz(func(t *testing.T, rootBytes, start, end, value, encoded []byte) {
		if len(rootBytes) > 64 || len(start) > 64 || len(end) > 64 ||
			len(value) > 256 || len(encoded) > 4096 {
			return
		}
		var root mpt.Root
		copy(root[:], rootBytes)
		limits := mpt.DefaultLimits()
		limits.MaxKeyBytes = 64
		limits.MaxValueBytes = 256
		limits.MaxProofBytes = 4096
		var nodes [][]byte
		if len(encoded) != 0 {
			nodes = [][]byte{encoded}
		}
		proof, err := mpt.RangeProofFromNodes(nodes, limits)
		if err != nil {
			return
		}
		items := []mpt.RangeItem{mpt.NewRangeItem(start, value)}
		_ = mpt.VerifyRawRange(
			context.Background(), root, start, end, items, proof, limits,
		)
	})
}

func FuzzRecoveredNodeRejectsHostileInput(f *testing.F) {
	f.Add([]byte{0xc2, 0x20, 0x01})
	f.Add([]byte{0x80})
	f.Add([]byte{0xff})

	f.Fuzz(func(t *testing.T, encoded []byte) {
		if len(encoded) > 4096 {
			return
		}
		limits := mpt.DefaultLimits()
		limits.MaxRecoveryBytes = 4096
		trie, err := mpt.LoadRawTrie(
			mpt.EmptyRoot(), newTestNodeStore(), limits,
		)
		if err != nil {
			t.Fatalf("LoadRawTrie() error = %v", err)
		}
		_, _ = trie.RecoverNode(
			context.Background(), testKeccakRoot(encoded), encoded,
		)
	})
}

func FuzzCollectReachableNodeRejectsHostileInput(f *testing.F) {
	f.Add([]byte{0xc0})
	f.Add([]byte{0xc2, 0x20, 0x01})

	f.Fuzz(func(t *testing.T, encoded []byte) {
		if len(encoded) == 0 || len(encoded) > 4096 {
			return
		}
		root := testKeccakRoot(encoded)
		store := newTestNodeStore()
		store.nodes[root] = append([]byte(nil), encoded...)
		limits := mpt.DefaultReachabilityLimits()
		limits.MaxBytes = 4096
		limits.MaxNodes = 64
		limits.MaxNodeReads = 64
		limits.MaxHashOperations = 64
		_, _ = mpt.CollectReachableNodes(
			context.Background(), []mpt.Root{root}, store, limits,
		)
	})
}

func FuzzRawTrieMutationAndIterationModel(f *testing.F) {
	f.Add([]byte{
		1, 'a', 1, '1',
		1, 'b', 1, '2',
		1, 'a', 0,
	})
	f.Add([]byte{0, 0, 1, 'x'})

	f.Fuzz(func(t *testing.T, operations []byte) {
		if len(operations) > 512 {
			return
		}
		limits := mpt.DefaultLimits()
		limits.MaxKeyBytes = 8
		limits.MaxValueBytes = 32
		trie, err := mpt.NewRawTrie(limits)
		if err != nil {
			t.Fatalf("NewRawTrie() error = %v", err)
		}
		model := make(map[string][]byte)
		for cursor, count := 0, 0; cursor < len(operations) && count < 64; count++ {
			keyLength := int(operations[cursor] % 9)
			cursor++
			if keyLength > len(operations)-cursor {
				break
			}
			key := append([]byte(nil), operations[cursor:cursor+keyLength]...)
			cursor += keyLength
			if cursor >= len(operations) {
				break
			}
			valueLength := int(operations[cursor] % 33)
			cursor++
			if valueLength > len(operations)-cursor {
				break
			}
			value := append([]byte(nil), operations[cursor:cursor+valueLength]...)
			cursor += valueLength

			if valueLength == 0 {
				next, deleteErr := trie.Delete(context.Background(), key)
				if _, exists := model[string(key)]; exists {
					if deleteErr != nil {
						t.Fatalf("Delete(%x) error = %v", key, deleteErr)
					}
					trie = next
					delete(model, string(key))
				} else if deleteErr == nil {
					t.Fatalf("Delete(%x) unexpectedly succeeded", key)
				}
			} else {
				trie, err = trie.Update(context.Background(), key, value)
				if err != nil {
					t.Fatalf("Update(%x) error = %v", key, err)
				}
				model[string(key)] = value
			}
		}

		var keys [][]byte
		err = trie.Iterate(
			context.Background(),
			mpt.IterationOptions{},
			func(entry mpt.Entry) error {
				key := entry.Key()
				keys = append(keys, key)
				if !bytes.Equal(entry.Value(), model[string(key)]) {
					t.Fatalf("Iterate(%x) value = %x, want %x", key, entry.Value(), model[string(key)])
				}
				return nil
			},
		)
		if err != nil {
			t.Fatalf("Iterate() error = %v", err)
		}
		if len(keys) != len(model) {
			t.Fatalf("Iterate() returned %d entries, want %d", len(keys), len(model))
		}
		if !slices.IsSortedFunc(keys, bytes.Compare) {
			t.Fatalf("iteration keys are not ordered: %x", keys)
		}
	})
}
