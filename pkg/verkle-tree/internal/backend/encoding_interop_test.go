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

	multiproof "github.com/crate-crypto/go-ipa"
	"github.com/crate-crypto/go-ipa/bandersnatch/fr"
	"github.com/crate-crypto/go-ipa/banderwagon"
	"github.com/crate-crypto/go-ipa/common"
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

func TestRustVerkleMultiProof(t *testing.T) {
	t.Parallel()

	transcriptLabel, fixtureProof := readMultiProofFixture(t)

	config, err := ipa.NewIPASettings()
	if err != nil {
		t.Fatal(err)
	}
	polynomials, points := interopMultiProofCorpus()
	commitments := make([]*banderwagon.Element, len(polynomials))
	for index := range polynomials {
		value := config.Commit(polynomials[index])
		commitments[index] = &value
	}

	proof, err := multiproof.CreateMultiProof(
		common.NewTranscript(transcriptLabel),
		config,
		commitments,
		polynomials,
		points,
	)
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := proof.Write(&encoded); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded.Bytes(), fixtureProof) {
		t.Fatal("multiproof differs across Go and Rust")
	}

	var decoded multiproof.MultiProof
	if err := decoded.Read(bytes.NewReader(fixtureProof)); err != nil {
		t.Fatal(err)
	}
	results := make([]*fr.Element, len(polynomials))
	for index := range polynomials {
		results[index] = &polynomials[index][points[index]]
	}
	ok, err := multiproof.CheckMultiProof(
		common.NewTranscript(transcriptLabel),
		config,
		&decoded,
		commitments,
		results,
		points,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("Go verifier rejected Rust multiproof")
	}
}

func TestRustVerkleMultiProofRejectsMutations(t *testing.T) {
	t.Parallel()

	transcriptLabel, fixtureProof := readMultiProofFixture(t)
	config, err := ipa.NewIPASettings()
	if err != nil {
		t.Fatal(err)
	}
	polynomials, points := interopMultiProofCorpus()
	commitments := make([]*banderwagon.Element, len(polynomials))
	results := make([]*fr.Element, len(polynomials))
	for index := range polynomials {
		value := config.Commit(polynomials[index])
		commitments[index] = &value
		results[index] = &polynomials[index][points[index]]
	}

	for offset := 0; offset < len(fixtureProof); offset += 32 {
		mutated := bytes.Clone(fixtureProof)
		mutated[offset] ^= 1
		var decoded multiproof.MultiProof
		if err := decoded.Read(bytes.NewReader(mutated)); err != nil {
			continue
		}
		ok, err := multiproof.CheckMultiProof(
			common.NewTranscript(transcriptLabel),
			config,
			&decoded,
			commitments,
			results,
			points,
		)
		if err == nil && ok {
			t.Fatalf("accepted multiproof mutation at byte %d", offset)
		}
	}

	var decoded multiproof.MultiProof
	if err := decoded.Read(bytes.NewReader(fixtureProof)); err != nil {
		t.Fatal(err)
	}
	ok, err := multiproof.CheckMultiProof(
		common.NewTranscript("wrong-transcript"),
		config,
		&decoded,
		commitments,
		results,
		points,
	)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("accepted multiproof under wrong transcript")
	}

	wrongResults := append([]*fr.Element(nil), results...)
	one := fr.One()
	wrongResult := new(fr.Element).Add(results[0], &one)
	wrongResults[0] = wrongResult
	ok, err = multiproof.CheckMultiProof(
		common.NewTranscript(transcriptLabel),
		config,
		&decoded,
		commitments,
		wrongResults,
		points,
	)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("accepted multiproof with wrong opening result")
	}
}

func readMultiProofFixture(t *testing.T) (string, []byte) {
	t.Helper()

	contents, err := os.ReadFile("testdata/rust-verkle-multiproof.tsv")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(contents), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("fixture rows = %d, want 2", len(lines))
	}
	if lines[0] != "corpus\ttranscript\tproof" {
		t.Fatalf("fixture header = %q", lines[0])
	}
	fields := strings.Split(lines[1], "\t")
	if len(fields) != 3 {
		t.Fatalf("fixture fields = %d, want 3", len(fields))
	}
	if fields[0] != "three-openings-v1" {
		t.Fatalf("fixture corpus = %q", fields[0])
	}
	if fields[1] != "verkle" {
		t.Fatalf("fixture transcript = %q", fields[1])
	}
	fixtureProof := decodeInteropHex(t, 2, "multiproof", fields[2])
	if len(fixtureProof) != 576 {
		t.Fatalf("fixture proof bytes = %d, want 576", len(fixtureProof))
	}
	return fields[1], fixtureProof
}

func interopMultiProofCorpus() ([][]fr.Element, []uint8) {
	polynomials := make([][]fr.Element, 3)
	for index := range polynomials {
		polynomials[index] = make([]fr.Element, common.VectorLength)
	}
	for index := 0; index < common.VectorLength; index++ {
		value := uint64(index + 1)
		polynomials[0][index].SetUint64(value)
		polynomials[1][index].SetUint64(value * value)
		polynomials[2][index].SetUint64(3*uint64(index) + 7)
	}
	return polynomials, []uint8{3, 3, 200}
}

func decodeInteropHex(t *testing.T, row int, kind, encoded string) []byte {
	t.Helper()

	decoded, err := hex.DecodeString(encoded)
	if err != nil {
		t.Fatalf("fixture row %d decode %s: %v", row, kind, err)
	}

	return decoded
}
