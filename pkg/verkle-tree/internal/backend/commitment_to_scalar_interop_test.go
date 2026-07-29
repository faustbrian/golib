package backend

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestRustVerkleCommitmentToScalarVectors(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile("testdata/rust-verkle-commitment-hashes.tsv")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	if len(lines) != 7 {
		t.Fatalf("fixture rows = %d, want 7", len(lines))
	}
	const header = "scalar_u64\tcommitment_be\tmapped_scalar_le"
	if lines[0] != header {
		t.Fatalf("fixture header = %q, want %q", lines[0], header)
	}

	for row, line := range lines[1:] {
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			t.Fatalf("fixture row %d fields = %d, want 3", row+2, len(fields))
		}
		t.Run(fields[0], func(t *testing.T) {
			t.Parallel()

			encodedCommitment := decodeInteropHex(t, row+2, "commitment", fields[1])
			want := decodeInteropHex(t, row+2, "mapped scalar", fields[2])
			if fields[0] == "identity" {
				if !bytes.Equal(want, make([]byte, scalarSize)) {
					t.Fatalf("identity scalar = %x, want zero", want)
				}
				if _, err := decodeCommitment(encodedCommitment); !errors.Is(err, errInvalidCommitment) {
					t.Fatalf("decode identity error = %v, want %v", err, errInvalidCommitment)
				}

				return
			}

			commitment, err := decodeCommitment(encodedCommitment)
			if err != nil {
				t.Fatalf("decode commitment: %v", err)
			}
			got := encodeScalar(commitmentToScalar(commitment))
			if !bytes.Equal(got[:], want) {
				t.Fatalf("mapped scalar = %x, want %x", got, want)
			}
		})
	}
}
