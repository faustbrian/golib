package merkletree_test

import (
	"context"
	"fmt"
	"testing"

	merkletree "github.com/faustbrian/golib/pkg/merkle-tree"
)

func BenchmarkComputeRoot(b *testing.B) {
	for _, size := range []int{1, 256, 16_384} {
		leaves := make([]merkletree.RawLeaf, size)
		for index := range leaves {
			value := make([]byte, 32)
			value[0] = byte(index)
			value[1] = byte(index >> 8)
			leaves[index] = merkletree.NewRawLeaf(value)
		}

		b.Run(fmt.Sprintf("leaves_%d", size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(size * 32))

			for range b.N {
				if _, err := merkletree.ComputeRoot(
					context.Background(),
					merkletree.CanonicalProfile(),
					leaves,
					merkletree.DefaultLimits(),
				); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
