package backend

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestRustVerkleEncodingVectors(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile("testdata/rust-verkle-encoding.tsv")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(contents), "\n"), "\n")
	if len(lines) != 6 {
		t.Fatalf("fixture rows = %d, want 6", len(lines))
	}
	if lines[0] != "scalar_u64\tscalar_le\tcommitment_be" {
		t.Fatalf("fixture header = %q", lines[0])
	}

	expectedScalars := [...]uint64{1, 2, 3, 255, 65_535}
	for index, line := range lines[1:] {
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			t.Fatalf("fixture row %d fields = %d, want 3", index+2, len(fields))
		}

		scalarValue, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			t.Fatalf("fixture row %d scalar: %v", index+2, err)
		}
		if scalarValue != expectedScalars[index] {
			t.Fatalf(
				"fixture row %d scalar = %d, want %d",
				index+2,
				scalarValue,
				expectedScalars[index],
			)
		}

		scalarBytes := decodeInteropHex(t, index+2, "scalar", fields[1])
		if len(scalarBytes) != scalarSize {
			t.Fatalf("fixture row %d scalar bytes = %d, want %d", index+2, len(scalarBytes), scalarSize)
		}
		if binary.LittleEndian.Uint64(scalarBytes[:8]) != scalarValue ||
			!bytes.Equal(scalarBytes[8:], make([]byte, scalarSize-8)) {
			t.Fatalf("fixture row %d scalar encoding does not encode %d", index+2, scalarValue)
		}
		decodedScalar, err := decodeScalar(scalarBytes)
		if err != nil {
			t.Fatalf("fixture row %d decode scalar: %v", index+2, err)
		}
		encodedScalar := encodeScalar(decodedScalar)
		if !bytes.Equal(encodedScalar[:], scalarBytes) {
			t.Fatalf("fixture row %d scalar round trip changed bytes", index+2)
		}

		commitmentBytes := decodeInteropHex(t, index+2, "commitment", fields[2])
		decodedCommitment, err := decodeCommitment(commitmentBytes)
		if err != nil {
			t.Fatalf("fixture row %d decode commitment: %v", index+2, err)
		}
		encodedCommitment := encodeCommitment(decodedCommitment)
		if !bytes.Equal(encodedCommitment[:], commitmentBytes) {
			t.Fatalf("fixture row %d commitment round trip changed bytes", index+2)
		}
	}
}

func decodeInteropHex(t *testing.T, row int, kind, encoded string) []byte {
	t.Helper()

	decoded, err := hex.DecodeString(encoded)
	if err != nil {
		t.Fatalf("fixture row %d decode %s: %v", row, kind, err)
	}

	return decoded
}
