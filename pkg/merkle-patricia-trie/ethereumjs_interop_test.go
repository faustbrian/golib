//go:build interoperability

package mpt_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os/exec"
	"testing"

	mpt "github.com/faustbrian/golib/pkg/merkle-patricia-trie"
)

type ethereumJSRequest struct {
	Secure     bool                  `json:"secure"`
	Operations []ethereumJSOperation `json:"operations"`
}

type ethereumJSOperation struct {
	Kind  string `json:"kind"`
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}

type ethereumJSResult struct {
	Root  string  `json:"root"`
	Value *string `json:"value"`
}

func TestEthereumJSDifferentialMutationTrace(t *testing.T) {
	t.Parallel()

	for _, secure := range []bool{false, true} {
		secure := secure
		t.Run(fmt.Sprintf("secure=%t", secure), func(t *testing.T) {
			t.Parallel()
			runEthereumJSDifferentialTrace(t, secure)
		})
	}
}

func runEthereumJSDifferentialTrace(t *testing.T, secure bool) {
	t.Helper()
	operations := ethereumJSOperations()
	request, err := json.Marshal(ethereumJSRequest{
		Secure:     secure,
		Operations: operations,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	command := exec.CommandContext(
		context.Background(),
		"node",
		"scripts/ethereumjs-oracle.mjs",
	)
	command.Stdin = bytes.NewReader(request)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("ethereumjs oracle error = %v: %s", err, output)
	}
	var oracleResults []ethereumJSResult
	if err := json.Unmarshal(output, &oracleResults); err != nil {
		t.Fatalf("json.Unmarshal() error = %v: %s", err, output)
	}
	if len(oracleResults) != len(operations) {
		t.Fatalf(
			"ethereumjs result count = %d, want %d",
			len(oracleResults),
			len(operations),
		)
	}

	var (
		update func(context.Context, []byte, []byte) error
		remove func(context.Context, []byte) error
		get    func(context.Context, []byte) ([]byte, error)
		root   func() (mpt.Root, error)
	)
	if secure {
		trie, newErr := mpt.NewSecureTrie(mpt.DefaultLimits())
		err = newErr
		update = func(ctx context.Context, key, value []byte) error {
			var updateErr error
			trie, updateErr = trie.Update(ctx, key, value)
			return updateErr
		}
		remove = func(ctx context.Context, key []byte) error {
			var deleteErr error
			trie, deleteErr = trie.Delete(ctx, key)
			return deleteErr
		}
		get = func(ctx context.Context, key []byte) ([]byte, error) {
			return trie.Get(ctx, key)
		}
		root = func() (mpt.Root, error) {
			return trie.Root()
		}
	} else {
		trie, newErr := mpt.NewRawTrie(mpt.DefaultLimits())
		err = newErr
		update = func(ctx context.Context, key, value []byte) error {
			var updateErr error
			trie, updateErr = trie.Update(ctx, key, value)
			return updateErr
		}
		remove = func(ctx context.Context, key []byte) error {
			var deleteErr error
			trie, deleteErr = trie.Delete(ctx, key)
			return deleteErr
		}
		get = func(ctx context.Context, key []byte) ([]byte, error) {
			return trie.Get(ctx, key)
		}
		root = func() (mpt.Root, error) {
			return trie.Root()
		}
	}
	if err != nil {
		t.Fatalf("NewTrie() error = %v", err)
	}
	for index, operation := range operations {
		key, decodeErr := hex.DecodeString(operation.Key)
		if decodeErr != nil {
			t.Fatalf("step %d key decode error = %v", index, decodeErr)
		}
		switch operation.Kind {
		case "put":
			value, valueErr := hex.DecodeString(operation.Value)
			if valueErr != nil {
				t.Fatalf("step %d value decode error = %v", index, valueErr)
			}
			err = update(context.Background(), key, value)
		case "delete":
			err = remove(context.Background(), key)
		default:
			t.Fatalf("step %d unsupported operation %q", index, operation.Kind)
		}
		if err != nil {
			t.Fatalf("step %d %s error = %v", index, operation.Kind, err)
		}

		rootValue, rootErr := root()
		if rootErr != nil {
			t.Fatalf("step %d Root() error = %v", index, rootErr)
		}
		if got := hex.EncodeToString(rootValue[:]); got != oracleResults[index].Root {
			t.Fatalf(
				"step %d operation = %#v, root = %s, ethereumjs = %s",
				index,
				operation,
				got,
				oracleResults[index].Root,
			)
		}
		got, getErr := get(context.Background(), key)
		if oracleResults[index].Value == nil {
			if getErr == nil {
				t.Fatalf("step %d Get() = %x, want absent", index, got)
			}
		} else {
			if getErr != nil {
				t.Fatalf("step %d Get() error = %v", index, getErr)
			}
			if want := *oracleResults[index].Value; hex.EncodeToString(got) != want {
				t.Fatalf("step %d value = %x, ethereumjs = %s", index, got, want)
			}
		}
	}
}

func ethereumJSOperations() []ethereumJSOperation {
	generator := rand.New(rand.NewPCG(0x6d7074, 0x6574686a73))
	keys := make([][]byte, 24)
	for index := range keys {
		key := make([]byte, generator.IntN(8)+1)
		for offset := range key {
			key[offset] = byte(generator.Uint32())
		}
		keys[index] = key
	}

	state := make(map[string]struct{})
	operations := make([]ethereumJSOperation, 0, 256)
	for range 256 {
		key := keys[generator.IntN(len(keys))]
		keyString := string(key)
		if _, present := state[keyString]; present && generator.IntN(4) == 0 {
			operations = append(operations, ethereumJSOperation{
				Kind: "delete",
				Key:  hex.EncodeToString(key),
			})
			delete(state, keyString)
			continue
		}
		value := make([]byte, generator.IntN(64)+1)
		for index := range value {
			value[index] = byte(generator.Uint32())
		}
		operations = append(operations, ethereumJSOperation{
			Kind:  "put",
			Key:   hex.EncodeToString(key),
			Value: hex.EncodeToString(value),
		})
		state[keyString] = struct{}{}
	}
	return operations
}
