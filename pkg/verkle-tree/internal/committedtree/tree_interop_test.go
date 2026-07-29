package committedtree

import (
	"bytes"
	"context"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

func TestBuildMatchesPinnedRustTreeRoots(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile("testdata/rust-verkle-tree-roots.tsv")
	if err != nil {
		t.Fatalf("read Rust tree roots: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(contents), "\n"), "\n")
	if len(lines) != 7 {
		t.Fatalf("fixture rows = %d, want 7", len(lines))
	}
	const header = "case\tentries\troot_commitment_be"
	if lines[0] != header {
		t.Fatalf("fixture header = %q, want %q", lines[0], header)
	}

	for row, line := range lines[1:] {
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			t.Fatalf("fixture row %d fields = %d, want 3", row+2, len(fields))
		}
		t.Run(fields[0], func(t *testing.T) {
			entries := decodeRootEntries(t, row+2, fields[1])
			tree, err := Build(
				context.Background(),
				entries,
				testLimits(),
				testCommitmentLimits(),
			)
			if err != nil {
				t.Fatalf("build tree: %v", err)
			}
			root, err := tree.Root()
			if err != nil {
				t.Fatalf("get root: %v", err)
			}
			want := decodeRootHex(t, row+2, "root", fields[2])
			if len(entries) == 0 {
				empty, identityErr := root.IsIdentity()
				if identityErr != nil {
					t.Fatalf("classify empty root: %v", identityErr)
				}
				if !empty || !bytes.Equal(want, make([]byte, 32)) {
					t.Fatalf("empty root mismatch: identity %t, Rust %x", empty, want)
				}
				return
			}
			got, err := root.Bytes()
			if err != nil {
				t.Fatalf("encode root: %v", err)
			}
			if !bytes.Equal(got[:], want) {
				t.Fatalf("root = %x, want Rust %x", got, want)
			}
		})
	}
}

func decodeRootEntries(t testing.TB, row int, encoded string) []Entry {
	t.Helper()

	if encoded == "-" {
		return nil
	}
	items := strings.Split(encoded, ",")
	entries := make([]Entry, len(items))
	for index, item := range items {
		parts := strings.Split(item, ":")
		if len(parts) != 2 {
			t.Fatalf("fixture row %d entry %d is malformed", row, index)
		}
		key := decodeRootHex(t, row, "key", parts[0])
		value := decodeRootHex(t, row, "value", parts[1])
		copy(entries[index].Key[:], key)
		copy(entries[index].Value[:], value)
	}

	return entries
}

func decodeRootHex(t testing.TB, row int, field string, encoded string) []byte {
	t.Helper()

	decoded, err := hex.DecodeString(encoded)
	if err != nil {
		t.Fatalf("fixture row %d %s: %v", row, field, err)
	}
	if len(decoded) != 32 {
		t.Fatalf("fixture row %d %s bytes = %d, want 32", row, field, len(decoded))
	}

	return decoded
}
