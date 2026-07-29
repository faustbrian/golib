//go:build ignore

// Command generate_vectors emits deterministic RFC 9162 SHA-256 vectors from
// transparency-dev/merkle v0.0.2. Run it from the module root with:
//
//	GOWORK=off go run ./testdata/generate_vectors.go
package main

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"

	"github.com/transparency-dev/merkle/rfc6962"
	"github.com/transparency-dev/merkle/testonly"
)

type fixture struct {
	Schema      uint64              `json:"schema"`
	Profile     string              `json:"profile"`
	Reference   reference           `json:"reference"`
	Leaves      []string            `json:"leaves_base64"`
	Roots       []string            `json:"roots_hex"`
	Inclusion   []inclusionVector   `json:"inclusion"`
	Consistency []consistencyVector `json:"consistency"`
}

type reference struct {
	Specification string `json:"specification"`
	Module        string `json:"module"`
	Version       string `json:"version"`
	Revision      string `json:"revision"`
}

type inclusionVector struct {
	TreeSize uint64   `json:"tree_size"`
	Index    uint64   `json:"index"`
	Siblings []string `json:"siblings_hex"`
}

type consistencyVector struct {
	OlderTreeSize uint64   `json:"older_tree_size"`
	NewerTreeSize uint64   `json:"newer_tree_size"`
	Nodes         []string `json:"nodes_hex"`
}

func main() {
	leaves := [][]byte{
		{},
		[]byte("a"),
		[]byte("b"),
		[]byte("abc"),
		{0x00, 0xff},
		[]byte("fifth leaf"),
		{0x80},
		[]byte("last"),
	}
	tree := testonly.New(rfc6962.DefaultHasher)
	tree.AppendData(leaves...)

	output := fixture{
		Schema:  1,
		Profile: "rfc9162-sha256-v1",
		Reference: reference{
			Specification: "RFC 9162 sections 2.1.1, 2.1.3.1, and 2.1.4.1",
			Module:        "github.com/transparency-dev/merkle",
			Version:       "v0.0.2",
			Revision:      "036047b5d2f7faf3b1ee643d391e60fe5b1defcf",
		},
		Leaves: make([]string, len(leaves)),
		Roots:  make([]string, len(leaves)+1),
	}
	for index, leaf := range leaves {
		output.Leaves[index] = base64.StdEncoding.EncodeToString(leaf)
	}
	for size := range output.Roots {
		output.Roots[size] = hex.EncodeToString(tree.HashAt(uint64(size)))
	}
	for _, selected := range [][2]uint64{
		{1, 0}, {3, 0}, {3, 2}, {5, 4}, {8, 3}, {8, 7},
	} {
		proof, err := tree.InclusionProof(selected[1], selected[0])
		if err != nil {
			panic(err)
		}
		output.Inclusion = append(output.Inclusion, inclusionVector{
			TreeSize: selected[0],
			Index:    selected[1],
			Siblings: encodeDigests(proof),
		})
	}
	for _, selected := range [][2]uint64{
		{1, 2}, {1, 8}, {3, 5}, {3, 8}, {5, 8}, {7, 8}, {8, 8},
	} {
		proof, err := tree.ConsistencyProof(selected[0], selected[1])
		if err != nil {
			panic(err)
		}
		output.Consistency = append(output.Consistency, consistencyVector{
			OlderTreeSize: selected[0],
			NewerTreeSize: selected[1],
			Nodes:         encodeDigests(proof),
		})
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		panic(err)
	}
}

func encodeDigests(digests [][]byte) []string {
	encoded := make([]string, len(digests))
	for index, digest := range digests {
		encoded[index] = hex.EncodeToString(digest)
	}

	return encoded
}
