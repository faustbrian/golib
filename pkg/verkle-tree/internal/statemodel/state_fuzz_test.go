package statemodel

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"
)

const fuzzUpdateBytes = 65

func FuzzSnapshotTransitions(f *testing.F) {
	f.Add([]byte(nil))
	f.Add(make([]byte, fuzzUpdateBytes))
	f.Add(bytes.Repeat([]byte{0xff}, fuzzUpdateBytes*2))

	f.Fuzz(func(t *testing.T, encoded []byte) {
		count := len(encoded) / fuzzUpdateBytes
		if count > 8 {
			return
		}

		snapshot, err := NewSnapshot(Limits{
			MaxBatchUpdates:   8,
			MaxEntries:        8,
			MaxTemporaryBytes: 4096,
		})
		if err != nil {
			t.Fatalf("new snapshot: %v", err)
		}

		updates := make([]Update, 0, count)
		expected := make(map[Key]Value, count)
		seen := make(map[Key]struct{}, count)
		duplicate := false
		for index := range count {
			offset := index * fuzzUpdateBytes
			var key Key
			copy(key[:], encoded[offset+1:offset+33])
			var value Value
			copy(value[:], encoded[offset+33:offset+65])

			if _, exists := seen[key]; exists {
				duplicate = true
			}
			seen[key] = struct{}{}
			if encoded[offset]&1 == 0 {
				updates = append(updates, Set(key, value))
				expected[key] = value
			} else {
				updates = append(updates, Delete(key))
				delete(expected, key)
			}
		}

		got, err := snapshot.Apply(context.Background(), updates)
		if duplicate {
			if !errors.Is(err, errDuplicateKey) {
				t.Fatalf("duplicate apply error = %v, want errDuplicateKey", err)
			}

			return
		}
		if err != nil {
			t.Fatalf("apply: %v", err)
		}

		keys, err := got.Keys(context.Background())
		if err != nil {
			t.Fatalf("keys: %v", err)
		}
		expectedKeys := make([]Key, 0, len(expected))
		for key := range expected {
			expectedKeys = append(expectedKeys, key)
		}
		slices.SortFunc(expectedKeys, compareKey)
		if !slices.Equal(keys, expectedKeys) {
			t.Fatalf("keys = %x, want %x", keys, expectedKeys)
		}
		for key, expectedValue := range expected {
			value, present, err := got.Get(context.Background(), key)
			if err != nil || !present || value != expectedValue {
				t.Fatalf(
					"get %x = %x, present %t, error %v; want %x",
					key,
					value,
					present,
					err,
					expectedValue,
				)
			}
		}
	})
}
