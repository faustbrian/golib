package externalsort_test

import (
	"bytes"
	"context"
	"fmt"
	"os"

	externalsort "github.com/faustbrian/golib/pkg/external-sort"
)

func Example() {
	parent, err := os.MkdirTemp("", "external-sort-example-")
	if err != nil {
		panic(err)
	}
	defer func() {
		if err := os.RemoveAll(parent); err != nil {
			panic(err)
		}
	}()

	factory, err := externalsort.NewFactory(externalsort.Config{
		ParentDirectory: parent,
		RecordBytes:     2,
		ChunkRecords:    2,
		MaximumRecords:  3,
	})
	if err != nil {
		panic(err)
	}
	store, err := factory.Open(
		context.Background(),
		bytes.Repeat([]byte{1}, externalsort.AES256KeyBytes),
	)
	if err != nil {
		panic(err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			panic(err)
		}
	}()

	for _, record := range [][]byte{{3, 3}, {1, 1}, {2, 2}} {
		if err := store.Add(context.Background(), record); err != nil {
			panic(err)
		}
	}
	if err := store.ForEachSorted(
		context.Background(),
		func(record []byte) error {
			fmt.Printf("%x\n", record)

			return nil
		},
	); err != nil {
		panic(err)
	}

	// Output:
	// 0101
	// 0202
	// 0303
}
