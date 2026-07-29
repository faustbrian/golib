package merkletree_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"testing"

	cbergoon "github.com/cbergoon/merkletree"
	merkletree "github.com/faustbrian/golib/pkg/merkle-tree"
	transparencyrfc6962 "github.com/transparency-dev/merkle/rfc6962"
	transparencytest "github.com/transparency-dev/merkle/testonly"
	txaty "github.com/txaty/go-merkletree"
	wealdtech "github.com/wealdtech/go-merkletree/v2"
)

var comparisonRootSink []byte

func BenchmarkNativeConstruction(b *testing.B) {
	for _, size := range []int{256, 16_384} {
		raw, owned, cbergoonContent, txatyBlocks := comparisonLeaves(size)

		b.Run(fmt.Sprintf("leaves_%d/merkle-tree-rfc9162", size), func(b *testing.B) {
			profile := mustRFC9162Profile(b)
			b.ReportAllocs()
			b.SetBytes(int64(size * 32))
			for b.Loop() {
				root, err := merkletree.ComputeRoot(
					context.Background(),
					profile,
					owned,
					merkletree.DefaultLimits(),
				)
				if err != nil {
					b.Fatal(err)
				}
				comparisonRootSink = root.Digest().Bytes()
			}
		})

		b.Run(fmt.Sprintf("leaves_%d/transparency-dev-rfc6962", size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(size * 32))
			for b.Loop() {
				tree := transparencytest.New(transparencyrfc6962.DefaultHasher)
				tree.AppendData(raw...)
				comparisonRootSink = tree.Hash()
			}
		})

		b.Run(fmt.Sprintf("leaves_%d/cbergoon-native", size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(size * 32))
			for b.Loop() {
				tree, err := cbergoon.NewTree(cbergoonContent)
				if err != nil {
					b.Fatal(err)
				}
				comparisonRootSink = tree.MerkleRoot()
			}
		})

		b.Run(fmt.Sprintf("leaves_%d/txaty-native-sequential", size), func(b *testing.B) {
			config := &txaty.Config{
				HashFunc: txatySHA256,
				Mode:     txaty.ModeTreeBuild,
			}
			b.ReportAllocs()
			b.SetBytes(int64(size * 32))
			for b.Loop() {
				tree, err := txaty.New(config, txatyBlocks)
				if err != nil {
					b.Fatal(err)
				}
				comparisonRootSink = tree.Root
			}
		})

		b.Run(fmt.Sprintf("leaves_%d/wealdtech-native", size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(size * 32))
			for b.Loop() {
				tree, err := wealdtech.NewTree(
					wealdtech.WithData(raw),
					wealdtech.WithHashType(comparisonSHA256{}),
				)
				if err != nil {
					b.Fatal(err)
				}
				comparisonRootSink = tree.Root()
			}
		})

		if size == 16_384 {
			b.Run(fmt.Sprintf("leaves_%d/txaty-native-parallel-2", size), func(b *testing.B) {
				config := &txaty.Config{
					HashFunc:      txatySHA256,
					Mode:          txaty.ModeTreeBuild,
					RunInParallel: true,
					NumRoutines:   2,
				}
				b.ReportAllocs()
				b.SetBytes(int64(size * 32))
				for b.Loop() {
					tree, err := txaty.New(config, txatyBlocks)
					if err != nil {
						b.Fatal(err)
					}
					comparisonRootSink = tree.Root
				}
			})
		}
	}
}

func mustRFC9162Profile(b *testing.B) merkletree.Profile {
	b.Helper()

	profile, err := merkletree.RFC9162Profile(merkletree.HashSHA256)
	if err != nil {
		b.Fatal(err)
	}

	return profile
}

func comparisonLeaves(
	count int,
) ([][]byte, []merkletree.RawLeaf, []cbergoon.Content, []txaty.DataBlock) {
	raw := make([][]byte, count)
	owned := make([]merkletree.RawLeaf, count)
	cbergoonContent := make([]cbergoon.Content, count)
	txatyBlocks := make([]txaty.DataBlock, count)
	for index := range raw {
		value := make([]byte, 32)
		value[0] = byte(index)
		value[1] = byte(index >> 8)
		value[2] = byte(index >> 16)
		raw[index] = value
		owned[index] = merkletree.NewRawLeaf(value)
		cbergoonContent[index] = comparisonContent(value)
		txatyBlocks[index] = comparisonBlock(value)
	}

	return raw, owned, cbergoonContent, txatyBlocks
}

type comparisonContent []byte

func (content comparisonContent) CalculateHash() ([]byte, error) {
	digest := sha256.Sum256(content)

	return digest[:], nil
}

func (content comparisonContent) Equals(other cbergoon.Content) (bool, error) {
	candidate, ok := other.(comparisonContent)

	return ok && bytes.Equal(content, candidate), nil
}

type comparisonBlock []byte

func (block comparisonBlock) Serialize() ([]byte, error) {
	return block, nil
}

func txatySHA256(value []byte) ([]byte, error) {
	digest := sha256.Sum256(value)

	return digest[:], nil
}

type comparisonSHA256 struct{}

func (comparisonSHA256) Hash(values ...[]byte) []byte {
	hasher := sha256.New()
	for _, value := range values {
		_, _ = hasher.Write(value)
	}

	return hasher.Sum(nil)
}

func (comparisonSHA256) HashName() string { return "sha2-256" }

func (comparisonSHA256) HashLength() int { return sha256.Size }
