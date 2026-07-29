package merkletree_test

import (
	"context"
	"fmt"

	merkletree "github.com/faustbrian/golib/pkg/merkle-tree"
)

func ExampleComputeRoot() {
	leaves := []merkletree.RawLeaf{
		merkletree.NewRawLeaf([]byte("first")),
		merkletree.NewRawLeaf([]byte("second")),
		merkletree.NewRawLeaf([]byte("third")),
	}

	root, err := merkletree.ComputeRoot(
		context.Background(),
		merkletree.CanonicalProfile(),
		leaves,
		merkletree.DefaultLimits(),
	)
	if err != nil {
		panic(err)
	}

	fmt.Printf("%d %x\n", root.TreeSize(), root.Digest().Bytes())
	// Output:
	// 3 c3651e541714c53d648ecc7baeca7fe2c36ef4fa65bcce24b1d71286437de566
}
