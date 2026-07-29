package backend

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/crate-crypto/go-ipa/ipa"
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

func TestRustVerkleGeneratorSet(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile("testdata/rust-verkle-generators.tsv")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(contents), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("fixture rows = %d, want 2", len(lines))
	}
	if lines[0] != "width\tseed\tcommitments_sha256" {
		t.Fatalf("fixture header = %q", lines[0])
	}

	fields := strings.Split(lines[1], "\t")
	if len(fields) != 3 {
		t.Fatalf("fixture fields = %d, want 3", len(fields))
	}
	width, err := strconv.ParseUint(fields[0], 10, 16)
	if err != nil {
		t.Fatalf("fixture width: %v", err)
	}
	if width != 256 {
		t.Fatalf("fixture width = %d, want 256", width)
	}
	if fields[1] != "eth_verkle_oct_2021" {
		t.Fatalf("fixture seed = %q", fields[1])
	}
	fixtureDigest := decodeInteropHex(t, 2, "generator digest", fields[2])
	if len(fixtureDigest) != sha256.Size {
		t.Fatalf("fixture digest bytes = %d, want %d", len(fixtureDigest), sha256.Size)
	}

	generators := ipa.GenerateRandomPoints(width)
	digest := sha256.New()
	for _, generator := range generators {
		encoded := encodeCommitment(commitment{element: generator})
		_, _ = digest.Write(encoded[:])
	}
	if !bytes.Equal(digest.Sum(nil), fixtureDigest) {
		t.Fatal("generator set differs across Go and Rust")
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
