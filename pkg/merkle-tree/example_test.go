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

func ExampleSnapshot_InclusionProof() {
	leaves := []merkletree.RawLeaf{
		merkletree.NewRawLeaf([]byte("first")),
		merkletree.NewRawLeaf([]byte("second")),
		merkletree.NewRawLeaf([]byte("third")),
	}
	snapshot, err := merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		leaves,
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		panic(err)
	}
	proof, err := snapshot.InclusionProof(
		context.Background(),
		1,
		merkletree.DefaultProofLimits(),
	)
	if err != nil {
		panic(err)
	}
	err = merkletree.VerifyInclusion(
		context.Background(),
		proof,
		leaves[1],
		merkletree.DefaultProofLimits(),
	)

	fmt.Printf("%d %d %v\n", proof.TreeSize(), proof.LeafIndex(), err)
	// Output:
	// 3 1 <nil>
}

func ExampleBuilder() {
	builder, err := merkletree.NewBuilder(
		merkletree.CanonicalProfile(),
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		panic(err)
	}
	if err := builder.Append(
		context.Background(),
		merkletree.NewRawLeaf([]byte("first")),
	); err != nil {
		panic(err)
	}
	if err := builder.AppendBatch(
		context.Background(),
		[]merkletree.RawLeaf{
			merkletree.NewRawLeaf([]byte("second")),
			merkletree.NewRawLeaf([]byte("third")),
		},
	); err != nil {
		panic(err)
	}
	snapshot, err := builder.Snapshot(context.Background())
	if err != nil {
		panic(err)
	}
	root, err := snapshot.Root()
	if err != nil {
		panic(err)
	}

	fmt.Printf("%d %x\n", root.TreeSize(), root.Digest().Bytes())
	// Output:
	// 3 c3651e541714c53d648ecc7baeca7fe2c36ef4fa65bcce24b1d71286437de566
}

func ExampleRootBuilder() {
	builder, err := merkletree.NewRootBuilder(
		merkletree.CanonicalProfile(),
		merkletree.DefaultLimits(),
	)
	if err != nil {
		panic(err)
	}
	if err := builder.AppendBatch(
		context.Background(),
		[]merkletree.RawLeaf{
			merkletree.NewRawLeaf([]byte("first")),
			merkletree.NewRawLeaf([]byte("second")),
			merkletree.NewRawLeaf([]byte("third")),
		},
	); err != nil {
		panic(err)
	}
	root, err := builder.Root(context.Background())
	if err != nil {
		panic(err)
	}

	fmt.Printf("%d %x\n", root.TreeSize(), root.Digest().Bytes())
	// Output:
	// 3 c3651e541714c53d648ecc7baeca7fe2c36ef4fa65bcce24b1d71286437de566
}

func ExampleSnapshot_ConsistencyProof() {
	leaves := []merkletree.RawLeaf{
		merkletree.NewRawLeaf([]byte("first")),
		merkletree.NewRawLeaf([]byte("second")),
		merkletree.NewRawLeaf([]byte("third")),
	}
	older, err := merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		leaves[:2],
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		panic(err)
	}
	newer, err := merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		leaves,
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		panic(err)
	}
	olderRoot, err := older.Root()
	if err != nil {
		panic(err)
	}
	proof, err := newer.ConsistencyProof(
		context.Background(),
		olderRoot,
		merkletree.DefaultConsistencyProofLimits(),
	)
	if err != nil {
		panic(err)
	}
	err = merkletree.VerifyConsistency(
		context.Background(),
		proof,
		merkletree.DefaultConsistencyProofLimits(),
	)

	fmt.Printf(
		"%d %d %d %v\n",
		proof.OlderTreeSize(),
		proof.NewerTreeSize(),
		len(proof.Nodes()),
		err,
	)
	// Output:
	// 2 3 1 <nil>
}

func ExampleSnapshot_MultiInclusionProof() {
	leaves := []merkletree.RawLeaf{
		merkletree.NewRawLeaf([]byte("first")),
		merkletree.NewRawLeaf([]byte("second")),
		merkletree.NewRawLeaf([]byte("third")),
		merkletree.NewRawLeaf([]byte("fourth")),
	}
	snapshot, err := merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		leaves,
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		panic(err)
	}
	proof, err := snapshot.MultiInclusionProof(
		context.Background(),
		[]uint64{3, 1},
		merkletree.DefaultMultiProofLimits(),
	)
	if err != nil {
		panic(err)
	}
	err = merkletree.VerifyMultiInclusion(
		context.Background(),
		proof,
		[]merkletree.RawLeaf{leaves[1], leaves[3]},
		merkletree.DefaultMultiProofLimits(),
	)

	fmt.Printf(
		"%v %d %v\n",
		proof.LeafIndexes(),
		len(proof.Frontier()),
		err,
	)
	// Output:
	// [1 3] 2 <nil>
}

func ExampleParseSnapshot() {
	snapshot, err := merkletree.NewSnapshot(
		context.Background(),
		merkletree.CanonicalProfile(),
		[]merkletree.RawLeaf{
			merkletree.NewRawLeaf([]byte("first")),
			merkletree.NewRawLeaf([]byte("second")),
		},
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		panic(err)
	}
	encoded, err := snapshot.MarshalBinary()
	if err != nil {
		panic(err)
	}
	restored, err := merkletree.ParseSnapshot(
		context.Background(),
		encoded,
		merkletree.DefaultSnapshotPersistenceLimits(),
	)
	if err != nil {
		panic(err)
	}
	builder, err := merkletree.ResumeBuilder(
		context.Background(),
		restored,
		uint64(len("first")+len("second")),
		merkletree.DefaultSnapshotLimits(),
	)
	if err != nil {
		panic(err)
	}
	if err := builder.Append(
		context.Background(),
		merkletree.NewRawLeaf([]byte("third")),
	); err != nil {
		panic(err)
	}
	resumed, err := builder.Snapshot(context.Background())
	if err != nil {
		panic(err)
	}
	root, err := resumed.Root()
	if err != nil {
		panic(err)
	}

	fmt.Println(root.TreeSize())
	// Output:
	// 3
}
