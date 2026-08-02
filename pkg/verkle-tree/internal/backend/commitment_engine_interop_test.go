package backend

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestCommitmentEngineMatchesPinnedRustVectors(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile("testdata/rust-verkle-vector-commitments.tsv")
	if err != nil {
		t.Fatalf("read Rust vector commitments: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(contents), "\n"), "\n")
	if len(lines) != 6 {
		t.Fatalf("fixture rows = %d, want 6", len(lines))
	}
	const header = "case\tnon_zero_terms\tcommitment_be\tmapped_scalar_le"
	if lines[0] != header {
		t.Fatalf("fixture header = %q, want %q", lines[0], header)
	}

	engine, err := NewCommitmentEngine(context.Background(), testCommitmentLimits())
	if err != nil {
		t.Fatalf("new commitment engine: %v", err)
	}
	for row, line := range lines[1:] {
		fields := strings.Split(line, "\t")
		if len(fields) != 4 {
			t.Fatalf("fixture row %d fields = %d, want 4", row+2, len(fields))
		}
		t.Run(fields[0], func(t *testing.T) {
			vector, terms := commitmentFixtureVector(t, fields[0])
			wantTerms, err := strconv.ParseUint(fields[1], 10, 16)
			if err != nil {
				t.Fatalf("parse non-zero terms: %v", err)
			}
			if terms != int(wantTerms) {
				t.Fatalf("fixture terms = %d, want %d", terms, wantTerms)
			}

			committed, err := engine.Commit(context.Background(), vector)
			if err != nil {
				t.Fatalf("commit vector: %v", err)
			}
			wantCommitment := decodeInteropHex(t, row+2, "commitment", fields[2])
			wantScalar := decodeInteropHex(t, row+2, "mapped scalar", fields[3])
			if terms == 0 {
				identity, identityErr := committed.IsIdentity()
				if identityErr != nil {
					t.Fatalf("classify commitment: %v", identityErr)
				}
				if !identity {
					t.Fatal("zero vector commitment is not identity")
				}
			} else {
				got, encodeErr := committed.Bytes()
				if encodeErr != nil {
					t.Fatalf("encode commitment: %v", encodeErr)
				}
				if !bytes.Equal(got[:], wantCommitment) {
					t.Fatalf("commitment = %x, want Rust %x", got, wantCommitment)
				}
			}
			got, mapErr := committed.ScalarBytes()
			if mapErr != nil {
				t.Fatalf("map commitment: %v", mapErr)
			}
			if !bytes.Equal(got[:], wantScalar) {
				t.Fatalf("mapped scalar = %x, want Rust %x", got, wantScalar)
			}
		})
	}
}

func TestCommitmentEngineSparseUpdateMatchesPinnedRustVector(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile("testdata/rust-verkle-vector-commitments.tsv")
	if err != nil {
		t.Fatalf("read Rust vector commitments: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(contents), "\n"), "\n")
	if len(lines) != 6 {
		t.Fatalf("fixture rows = %d, want 6", len(lines))
	}
	sparseFields := strings.Split(lines[4], "\t")
	if len(sparseFields) != 4 || sparseFields[0] != "sparse-boundaries" {
		t.Fatalf("sparse fixture row = %q", lines[4])
	}
	want := decodeInteropHex(t, 5, "commitment", sparseFields[2])

	engine, err := NewCommitmentEngine(context.Background(), testCommitmentLimits())
	if err != nil {
		t.Fatalf("new commitment engine: %v", err)
	}
	before, _ := commitmentFixtureVector(t, "one-hot-first")
	after, _ := commitmentFixtureVector(t, "sparse-boundaries")
	committed, err := engine.Commit(context.Background(), before)
	if err != nil {
		t.Fatalf("commit pre-update vector: %v", err)
	}
	updates := make([]VectorUpdate, 0, 4)
	for _, index := range []uint8{1, 127, 128, 255} {
		updates = append(updates, VectorUpdate{
			Index: index,
			Old:   before[index],
			New:   after[index],
		})
	}
	updated, err := engine.UpdateCommitment(
		context.Background(), committed, updates,
	)
	if err != nil {
		t.Fatalf("update commitment: %v", err)
	}
	got, err := updated.Bytes()
	if err != nil {
		t.Fatalf("encode updated commitment: %v", err)
	}
	if !bytes.Equal(got[:], want) {
		t.Fatalf("updated commitment = %x, want Rust %x", got, want)
	}
}

func commitmentFixtureVector(t testing.TB, name string) (Vector, int) {
	t.Helper()

	var vector Vector
	switch name {
	case "zero":
		return vector, 0
	case "one-hot-first":
		setVectorUint64(&vector, 0, 1)
		return vector, 1
	case "one-hot-last":
		setVectorUint64(&vector, 255, 2)
		return vector, 1
	case "sparse-boundaries":
		for _, item := range []struct {
			index int
			value uint64
		}{{0, 1}, {1, 2}, {127, 3}, {128, 255}, {255, 65_535}} {
			setVectorUint64(&vector, item.index, item.value)
		}
		return vector, 5
	case "dense-incrementing":
		for index := range vector {
			setVectorUint64(&vector, index, uint64(index+1))
		}
		return vector, VectorWidth
	default:
		t.Fatalf("unknown commitment fixture case %q", name)
		return Vector{}, 0
	}
}

func setVectorUint64(vector *Vector, index int, value uint64) {
	binary.LittleEndian.PutUint64(vector[index][:8], value)
}
