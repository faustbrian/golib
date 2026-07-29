package authstate

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/statemodel"
)

const fuzzUpdateBytes = 65

func FuzzSnapshotTransitions(f *testing.F) {
	f.Add([]byte(nil))
	f.Add(make([]byte, fuzzUpdateBytes))
	f.Add(bytes.Repeat([]byte{0xff}, fuzzUpdateBytes*2))

	base := newTestSnapshot(f, nil)
	oracleBase, err := statemodel.NewSnapshot(statemodel.Limits{
		MaxBatchUpdates:   8,
		MaxEntries:        8,
		MaxTemporaryBytes: 1 << 20,
	})
	if err != nil {
		f.Fatalf("new reference snapshot: %v", err)
	}

	f.Fuzz(func(t *testing.T, encoded []byte) {
		count := len(encoded) / fuzzUpdateBytes
		if count > 8 {
			return
		}

		updates := make([]Update, 0, count)
		oracleUpdates := make([]statemodel.Update, 0, count)
		keys := make([]Key, 0, count)
		for index := range count {
			offset := index * fuzzUpdateBytes
			var key Key
			copy(key[:], encoded[offset+1:offset+33])
			var value Value
			copy(value[:], encoded[offset+33:offset+65])
			keys = append(keys, key)

			if encoded[offset]&1 == 0 {
				updates = append(updates, Set(key, value))
				oracleUpdates = append(
					oracleUpdates,
					statemodel.Set(statemodel.Key(key), statemodel.Value(value)),
				)
			} else {
				updates = append(updates, Delete(key))
				oracleUpdates = append(oracleUpdates, statemodel.Delete(statemodel.Key(key)))
			}
		}

		got, _, gotErr := base.Apply(context.Background(), updates)
		want, wantErr := oracleBase.Apply(context.Background(), oracleUpdates)
		if (gotErr != nil) != (wantErr != nil) {
			t.Fatalf("transition classification differs: authenticated %v, reference %v", gotErr, wantErr)
		}
		if gotErr != nil {
			if !errors.Is(gotErr, errDuplicateKey) {
				t.Fatalf("transition errors: authenticated %v, reference %v", gotErr, wantErr)
			}
			return
		}

		for _, key := range keys {
			value, present, getErr := got.Get(context.Background(), key)
			wantValue, wantPresent, referenceErr := want.Get(
				context.Background(), statemodel.Key(key),
			)
			if getErr != nil || referenceErr != nil || present != wantPresent ||
				statemodel.Value(value) != wantValue {
				t.Fatalf(
					"key %x = %x/%t/%v, want %x/%t/%v",
					key,
					value,
					present,
					getErr,
					wantValue,
					wantPresent,
					referenceErr,
				)
			}
		}

		reversed := slices.Clone(updates)
		slices.Reverse(reversed)
		reordered, _, err := base.Apply(context.Background(), reversed)
		if err != nil {
			t.Fatalf("apply reordered transition: %v", err)
		}
		assertSameSnapshotRoot(t, got, reordered)
	})
}
