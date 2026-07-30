package mpt_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
)

type fuzzMutation struct {
	key    []byte
	value  []byte
	delete bool
}

func FuzzSortedBuilderMutationHistoryParity(f *testing.F) {
	f.Add([]byte{1, 'a', 0, '1', 1, 'b', 0, '2'})
	f.Add([]byte{1, 'b', 0, '1', 1, 'a', 0, '2'})
	f.Add([]byte{0, 0, 'x'})

	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 512 {
			return
		}
		entries := decodeFuzzBuilderEntries(input, 64)
		limits := fuzzLimits()
		builder, err := mpt.NewSortedBuilder(limits)
		if err != nil {
			t.Fatalf("NewSortedBuilder() error = %v", err)
		}
		trie, err := mpt.NewRawTrie(limits)
		if err != nil {
			t.Fatalf("NewRawTrie() error = %v", err)
		}

		var previous []byte
		hasPrevious := false
		for _, entry := range entries {
			err := builder.Add(context.Background(), entry.key, entry.value)
			if hasPrevious {
				switch comparison := bytes.Compare(entry.key, previous); {
				case comparison == 0:
					if !errors.Is(err, mpt.ErrDuplicateBuilderKey) {
						t.Fatalf("duplicate Add(%x) error = %v", entry.key, err)
					}
					continue
				case comparison < 0:
					if !errors.Is(err, mpt.ErrOutOfOrderKey) {
						t.Fatalf("out-of-order Add(%x) error = %v", entry.key, err)
					}
					continue
				}
			}
			if err != nil {
				t.Fatalf("Add(%x) error = %v", entry.key, err)
			}
			trie, err = trie.Update(context.Background(), entry.key, entry.value)
			if err != nil {
				t.Fatalf("Update(%x) error = %v", entry.key, err)
			}
			previous = append(previous[:0], entry.key...)
			hasPrevious = true
		}

		got, err := builder.Finalize(context.Background())
		if err != nil {
			t.Fatalf("Finalize() error = %v", err)
		}
		want, err := trie.Root()
		if err != nil {
			t.Fatalf("Root() error = %v", err)
		}
		if got != want {
			t.Fatalf("builder root = %x, ordinary root = %x", got, want)
		}
		if _, err := builder.Finalize(
			context.Background(),
		); !errors.Is(err, mpt.ErrClosedBuilder) {
			t.Fatalf("second Finalize() error = %v", err)
		}
	})
}

func FuzzRawAndSecureRebuildMutationParity(f *testing.F) {
	f.Add([]byte{0x01, 'a', 0, '1', 0x01, 'b', 0, '2'})
	f.Add([]byte{0x01, 'a', 0, '1', 0x81, 'a'})
	f.Add([]byte{0x00, 0, 'x'})

	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 512 {
			return
		}
		mutations := decodeFuzzMutations(input, 64)
		limits := fuzzLimits()
		raw, err := mpt.NewRawTrie(limits)
		if err != nil {
			t.Fatalf("NewRawTrie() error = %v", err)
		}
		secure, err := mpt.NewSecureTrie(limits)
		if err != nil {
			t.Fatalf("NewSecureTrie() error = %v", err)
		}
		model := make(map[string][]byte)
		for _, mutation := range mutations {
			if mutation.delete {
				if _, exists := model[string(mutation.key)]; !exists {
					continue
				}
				raw, err = raw.Delete(context.Background(), mutation.key)
				if err != nil {
					t.Fatalf("RawTrie.Delete(%x) error = %v", mutation.key, err)
				}
				secure, err = secure.Delete(context.Background(), mutation.key)
				if err != nil {
					t.Fatalf("SecureTrie.Delete(%x) error = %v", mutation.key, err)
				}
				delete(model, string(mutation.key))
				continue
			}
			raw, err = raw.Update(context.Background(), mutation.key, mutation.value)
			if err != nil {
				t.Fatalf("RawTrie.Update(%x) error = %v", mutation.key, err)
			}
			secure, err = secure.Update(
				context.Background(),
				mutation.key,
				mutation.value,
			)
			if err != nil {
				t.Fatalf("SecureTrie.Update(%x) error = %v", mutation.key, err)
			}
			model[string(mutation.key)] = append([]byte(nil), mutation.value...)
		}

		rebuiltRaw, err := raw.Rebuild(context.Background())
		if err != nil {
			t.Fatalf("RawTrie.Rebuild() error = %v", err)
		}
		rebuiltSecure, err := secure.Rebuild(context.Background())
		if err != nil {
			t.Fatalf("SecureTrie.Rebuild() error = %v", err)
		}
		assertRawRootsEqual(t, raw, rebuiltRaw)
		assertSecureRootsEqual(t, secure, rebuiltSecure)
		for key, want := range model {
			got, err := rebuiltRaw.Get(context.Background(), []byte(key))
			if err != nil || !slices.Equal(got, want) {
				t.Fatalf("rebuilt raw Get(%x) = (%x, %v), want %x", key, got, err, want)
			}
			got, err = rebuiltSecure.Get(context.Background(), []byte(key))
			if err != nil || !slices.Equal(got, want) {
				t.Fatalf("rebuilt secure Get(%x) = (%x, %v), want %x", key, got, err, want)
			}
		}
	})
}

func FuzzEIP1186AccountAndStorageVerification(f *testing.F) {
	f.Add(
		[]byte("account address"),
		[]byte("storage slot"),
		[]byte{0x2a},
		[]byte("code"),
		[]byte{0xc0},
	)
	f.Add(
		[]byte(nil),
		[]byte(nil),
		[]byte(nil),
		[]byte(nil),
		[]byte{0xff},
	)

	f.Fuzz(func(
		t *testing.T,
		addressInput, slotInput, storageInput, codeInput, hostileNode []byte,
	) {
		if len(addressInput) > 64 || len(slotInput) > 64 ||
			len(storageInput) > mpt.RootBytes || len(codeInput) > 64 ||
			len(hostileNode) > 4096 {
			return
		}
		limits := fuzzLimits()
		limits.MaxKeyBytes = mpt.RootBytes
		var address [20]byte
		copy(address[:], addressInput)
		var slot [mpt.RootBytes]byte
		copy(slot[:], slotInput)
		storageValue := bytes.TrimLeft(storageInput, "\x00")

		storageTrie, err := mpt.NewSecureTrie(limits)
		if err != nil {
			t.Fatalf("NewSecureTrie(storage) error = %v", err)
		}
		if len(storageValue) != 0 {
			storageTrie, err = storageTrie.Update(
				context.Background(),
				slot[:],
				mustRLPString(t, storageValue),
			)
			if err != nil {
				t.Fatalf("storage Update() error = %v", err)
			}
		}
		storageRoot := mustSecureRoot(t, storageTrie)
		storageProof, err := storageTrie.Prove(context.Background(), slot[:])
		if err != nil {
			t.Fatalf("storage Prove() error = %v", err)
		}

		var balance [mpt.RootBytes]byte
		copy(balance[mpt.RootBytes-len(storageInput):], storageInput)
		accountValue, err := mpt.NewAccountValue(
			uint64(len(addressInput)),
			balance,
			storageRoot,
			[mpt.RootBytes]byte(testKeccakRoot(codeInput)),
			limits,
		)
		if err != nil {
			t.Fatalf("NewAccountValue() error = %v", err)
		}
		stateTrie, err := mpt.NewSecureTrie(limits)
		if err != nil {
			t.Fatalf("NewSecureTrie(state) error = %v", err)
		}
		stateTrie, err = stateTrie.Update(
			context.Background(),
			address[:],
			accountValue.Bytes(),
		)
		if err != nil {
			t.Fatalf("state Update() error = %v", err)
		}
		stateRoot := mustSecureRoot(t, stateTrie)
		accountProof, err := stateTrie.Prove(context.Background(), address[:])
		if err != nil {
			t.Fatalf("account Prove() error = %v", err)
		}
		account, err := mpt.VerifyAccountProof(
			context.Background(),
			stateRoot,
			address,
			accountValue.Bytes(),
			accountProof,
			limits,
		)
		if err != nil {
			t.Fatalf("VerifyAccountProof() error = %v", err)
		}
		if err := mpt.VerifyStorageProof(
			context.Background(),
			account,
			slot,
			storageValue,
			storageProof,
			limits,
		); err != nil {
			t.Fatalf("VerifyStorageProof() error = %v", err)
		}

		absentAddress := address
		absentAddress[0] ^= 0xff
		absenceProof, err := stateTrie.Prove(
			context.Background(),
			absentAddress[:],
		)
		if err != nil {
			t.Fatalf("absent account Prove() error = %v", err)
		}
		if err := mpt.VerifyAccountAbsence(
			context.Background(),
			stateRoot,
			absentAddress,
			absenceProof,
			limits,
		); err != nil {
			t.Fatalf("VerifyAccountAbsence() error = %v", err)
		}

		if len(hostileNode) != 0 {
			nodes := append(accountProof.Nodes(), hostileNode)
			if hostileProof, proofErr := mpt.ProofFromNodes(
				nodes,
				limits,
			); proofErr == nil {
				if _, verifyErr := mpt.VerifyAccountProof(
					context.Background(),
					stateRoot,
					address,
					accountValue.Bytes(),
					hostileProof,
					limits,
				); verifyErr == nil {
					t.Fatal("VerifyAccountProof() accepted a surplus hostile node")
				}
			}
		}
	})
}

func FuzzCommitRecoveryStateMachine(f *testing.F) {
	f.Add([]byte("first"), []byte("second"), byte(0))
	f.Add([]byte(nil), []byte(nil), byte(1))

	f.Fuzz(func(t *testing.T, first, second []byte, selector byte) {
		if len(first) > 64 || len(second) > 64 {
			return
		}
		limits := fuzzLimits()
		firstValue := longFuzzValue(first, 0x11)
		secondValue := longFuzzValue(second, 0x22)
		trie, err := mpt.NewRawTrie(limits)
		if err != nil {
			t.Fatalf("NewRawTrie() error = %v", err)
		}
		trie, err = trie.Update(context.Background(), []byte("alpha"), firstValue)
		if err != nil {
			t.Fatalf("Update(alpha) error = %v", err)
		}
		trie, err = trie.Update(context.Background(), []byte("beta"), secondValue)
		if err != nil {
			t.Fatalf("Update(beta) error = %v", err)
		}
		store := newTestNodeStore()
		committed, err := trie.Commit(context.Background(), store)
		if err != nil {
			t.Fatalf("Commit() error = %v", err)
		}
		root := mustTrieRoot(t, committed)
		encoded, exists := store.nodes[root]
		if !exists {
			t.Fatalf("committed root node %x is absent", root)
		}
		encoded = append([]byte(nil), encoded...)
		delete(store.nodes, root)

		loaded, err := mpt.LoadRawTrie(root, store, limits)
		if err != nil {
			t.Fatalf("LoadRawTrie() error = %v", err)
		}
		if _, err := loaded.Get(
			context.Background(),
			[]byte("alpha"),
		); !errors.Is(err, mpt.ErrMissingNode) {
			t.Fatalf("missing Get() error = %v", err)
		}
		corrupt := append([]byte(nil), encoded...)
		corrupt[int(selector)%len(corrupt)] ^= 0x01
		if _, err := loaded.RecoverNode(
			context.Background(),
			root,
			corrupt,
		); !errors.Is(err, mpt.ErrCorruptNode) {
			t.Fatalf("corrupt RecoverNode() error = %v", err)
		}

		recovered, err := loaded.RecoverNode(context.Background(), root, encoded)
		if err != nil {
			t.Fatalf("RecoverNode() error = %v", err)
		}
		recoveredAgain, err := recovered.RecoverNode(
			context.Background(),
			root,
			encoded,
		)
		if err != nil {
			t.Fatalf("idempotent RecoverNode() error = %v", err)
		}
		assertRawRootsEqual(t, recovered, recoveredAgain)
		if got, err := recovered.Get(
			context.Background(),
			[]byte("alpha"),
		); err != nil || !slices.Equal(got, firstValue) {
			t.Fatalf("recovered Get(alpha) = (%x, %v), want %x", got, err, firstValue)
		}
		if _, err := recovered.Commit(context.Background(), store); err != nil {
			t.Fatalf("recovery Commit() error = %v", err)
		}
		reloaded, err := mpt.LoadRawTrie(root, store, limits)
		if err != nil {
			t.Fatalf("LoadRawTrie(repaired) error = %v", err)
		}
		if got, err := reloaded.Get(
			context.Background(),
			[]byte("beta"),
		); err != nil || !slices.Equal(got, secondValue) {
			t.Fatalf("reloaded Get(beta) = (%x, %v), want %x", got, err, secondValue)
		}
	})
}

func FuzzIterationCancellationAndCallbackFailure(f *testing.F) {
	f.Add([]byte{0x01, 'a', 0, '1'}, byte(0), false)
	f.Add([]byte{0x01, 'a', 0, '1'}, byte(1), true)

	f.Fuzz(func(t *testing.T, input []byte, stopByte byte, canceled bool) {
		if len(input) > 512 {
			return
		}
		limits := fuzzLimits()
		trie, model := applyFuzzMutations(t, input, limits)
		if canceled {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			err := trie.Iterate(ctx, mpt.IterationOptions{}, func(mpt.Entry) error {
				t.Fatal("canceled iteration invoked its callback")
				return nil
			})
			if !errors.Is(err, context.Canceled) ||
				!errors.Is(err, mpt.ErrCanceled) {
				t.Fatalf("canceled Iterate() error = %v", err)
			}
			return
		}

		stop := int(stopByte) % (len(model) + 1)
		callbackErr := errors.New("fuzz callback stopped iteration")
		count := 0
		err := trie.Iterate(
			context.Background(),
			mpt.IterationOptions{},
			func(mpt.Entry) error {
				if count == stop {
					count++
					return callbackErr
				}
				count++
				return nil
			},
		)
		if stop < len(model) {
			if !errors.Is(err, callbackErr) || count != stop+1 {
				t.Fatalf(
					"callback-stopped Iterate() = (count %d, error %v), want (%d, %v)",
					count,
					err,
					stop+1,
					callbackErr,
				)
			}
		} else if err != nil || count != len(model) {
			t.Fatalf(
				"complete Iterate() = (count %d, error %v), want (%d, nil)",
				count,
				err,
				len(model),
			)
		}
	})
}

func fuzzLimits() mpt.Limits {
	limits := mpt.DefaultLimits()
	limits.MaxKeyBytes = 8
	limits.MaxValueBytes = 128
	return limits
}

func decodeFuzzBuilderEntries(input []byte, maximum int) []fuzzMutation {
	entries := make([]fuzzMutation, 0, maximum)
	for cursor := 0; cursor < len(input) && len(entries) < maximum; {
		keyLength := int(input[cursor] % 9)
		cursor++
		if keyLength > len(input)-cursor {
			break
		}
		key := append([]byte(nil), input[cursor:cursor+keyLength]...)
		cursor += keyLength
		if cursor >= len(input) {
			break
		}
		valueLength := int(input[cursor]%16) + 1
		cursor++
		if valueLength > len(input)-cursor {
			break
		}
		value := append([]byte(nil), input[cursor:cursor+valueLength]...)
		cursor += valueLength
		entries = append(entries, fuzzMutation{key: key, value: value})
	}
	return entries
}

func decodeFuzzMutations(input []byte, maximum int) []fuzzMutation {
	mutations := make([]fuzzMutation, 0, maximum)
	for cursor := 0; cursor < len(input) && len(mutations) < maximum; {
		control := input[cursor]
		cursor++
		keyLength := int((control & 0x0f) % 9)
		if keyLength > len(input)-cursor {
			break
		}
		key := append([]byte(nil), input[cursor:cursor+keyLength]...)
		cursor += keyLength
		if control&0x80 != 0 {
			mutations = append(mutations, fuzzMutation{key: key, delete: true})
			continue
		}
		if cursor >= len(input) {
			break
		}
		valueLength := int(input[cursor]%32) + 1
		cursor++
		if valueLength > len(input)-cursor {
			break
		}
		value := append([]byte(nil), input[cursor:cursor+valueLength]...)
		cursor += valueLength
		mutations = append(mutations, fuzzMutation{key: key, value: value})
	}
	return mutations
}

func applyFuzzMutations(
	t *testing.T,
	input []byte,
	limits mpt.Limits,
) (mpt.RawTrie, map[string][]byte) {
	t.Helper()
	trie, err := mpt.NewRawTrie(limits)
	if err != nil {
		t.Fatalf("NewRawTrie() error = %v", err)
	}
	model := make(map[string][]byte)
	for _, mutation := range decodeFuzzMutations(input, 64) {
		if mutation.delete {
			if _, exists := model[string(mutation.key)]; !exists {
				continue
			}
			trie, err = trie.Delete(context.Background(), mutation.key)
			if err != nil {
				t.Fatalf("Delete(%x) error = %v", mutation.key, err)
			}
			delete(model, string(mutation.key))
			continue
		}
		trie, err = trie.Update(context.Background(), mutation.key, mutation.value)
		if err != nil {
			t.Fatalf("Update(%x) error = %v", mutation.key, err)
		}
		model[string(mutation.key)] = append([]byte(nil), mutation.value...)
	}
	return trie, model
}

func assertRawRootsEqual(t *testing.T, left, right mpt.RawTrie) {
	t.Helper()
	leftRoot, err := left.Root()
	if err != nil {
		t.Fatalf("left Root() error = %v", err)
	}
	rightRoot, err := right.Root()
	if err != nil {
		t.Fatalf("right Root() error = %v", err)
	}
	if leftRoot != rightRoot {
		t.Fatalf("roots differ: left %x, right %x", leftRoot, rightRoot)
	}
}

func assertSecureRootsEqual(t *testing.T, left, right mpt.SecureTrie) {
	t.Helper()
	leftRoot, err := left.Root()
	if err != nil {
		t.Fatalf("left Root() error = %v", err)
	}
	rightRoot, err := right.Root()
	if err != nil {
		t.Fatalf("right Root() error = %v", err)
	}
	if leftRoot != rightRoot {
		t.Fatalf("secure roots differ: left %x, right %x", leftRoot, rightRoot)
	}
}

func longFuzzValue(seed []byte, fill byte) []byte {
	value := bytes.Repeat([]byte{fill}, 48)
	copy(value, seed)
	return value
}
