package verkletree_test

import (
	"context"
	"fmt"

	verkletree "github.com/faustbrian/golib/pkg/verkle-tree"
)

func ExampleSnapshot() {
	ctx := context.Background()
	key := verkletree.Key{}
	snapshot, err := verkletree.NewSnapshot(
		ctx,
		verkletree.BandersnatchIPA256V0(),
		[]verkletree.Entry{{Key: key, Value: verkletree.Value{}}},
		publicSnapshotLimits(),
	)
	if err != nil {
		panic(err)
	}

	value, present, err := snapshot.Get(ctx, key)
	if err != nil {
		panic(err)
	}
	root, err := snapshot.Root()
	if err != nil {
		panic(err)
	}
	_, err = root.Bytes()
	if err != nil {
		panic(err)
	}

	fmt.Println(present, value == (verkletree.Value{}))
	// Output: true true
}
