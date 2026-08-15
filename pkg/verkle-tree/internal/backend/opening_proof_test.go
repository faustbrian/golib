package backend

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"testing"
)

func TestOpeningProofMatchesPinnedRustFixture(t *testing.T) {
	t.Parallel()

	_, fixture := readMultiProofFixture(t)
	original := bytes.Clone(fixture)
	proof, err := DecodeOpeningProof(
		context.Background(), fixture, testOpeningProofLimits(),
	)
	if err != nil {
		t.Fatalf("decode opening proof: %v", err)
	}
	fixture[0] ^= 1
	encoded, err := proof.Bytes()
	if err != nil {
		t.Fatalf("encode opening proof: %v", err)
	}
	if !bytes.Equal(encoded[:], original) {
		t.Fatalf("opening proof = %x, want pinned Rust fixture", encoded)
	}
}

func TestOpeningProofRejectsMalformedEncodings(t *testing.T) {
	t.Parallel()

	_, fixture := readMultiProofFixture(t)
	wrongSubgroup, err := hex.DecodeString(
		"280e608d5bbbe84b16aac62aa450e8921840ea563f1c9c266e0240d89cbe6a78",
	)
	if err != nil {
		t.Fatalf("decode wrong-subgroup fixture: %v", err)
	}
	modulus, err := hex.DecodeString(
		"e1e77628b506fd747104197400878fff007668020276ce0c525f67cad469fb1c",
	)
	if err != nil {
		t.Fatalf("decode modulus: %v", err)
	}

	nonCanonical := bytes.Clone(fixture)
	copy(nonCanonical[:commitmentSize], bytes.Repeat([]byte{0xff}, commitmentSize))
	wrongSubgroupProof := bytes.Clone(fixture)
	copy(wrongSubgroupProof[:commitmentSize], wrongSubgroup)
	nonCanonicalScalar := bytes.Clone(fixture)
	copy(nonCanonicalScalar[len(nonCanonicalScalar)-scalarSize:], modulus)
	tests := map[string]struct {
		encoded []byte
		limits  OpeningProofLimits
	}{
		"empty":                {encoded: nil, limits: testOpeningProofLimits()},
		"short":                {encoded: fixture[:len(fixture)-1], limits: testOpeningProofLimits()},
		"trailing byte":        {encoded: append(bytes.Clone(fixture), 0), limits: OpeningProofLimits{MaxProofBytes: OpeningProofSize + 1, MaxPointDecodes: openingProofPointCount, MaxScalarDecodes: 1}},
		"non-canonical point":  {encoded: nonCanonical, limits: testOpeningProofLimits()},
		"wrong-subgroup point": {encoded: wrongSubgroupProof, limits: testOpeningProofLimits()},
		"non-canonical scalar": {encoded: nonCanonicalScalar, limits: testOpeningProofLimits()},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := DecodeOpeningProof(
				context.Background(), test.encoded, test.limits,
			)
			if !errors.Is(err, errInvalidOpeningProof) {
				t.Fatalf("decode error = %v, want %v", err, errInvalidOpeningProof)
			}
		})
	}
}

func TestOpeningProofAcceptsCanonicalIdentityElements(t *testing.T) {
	t.Parallel()

	_, fixture := readMultiProofFixture(t)
	for index := 0; index < openingProofPointCount; index++ {
		encoded := bytes.Clone(fixture)
		start := index * commitmentSize
		clear(encoded[start : start+commitmentSize])
		proof, err := DecodeOpeningProof(
			context.Background(), encoded, testOpeningProofLimits(),
		)
		if err != nil {
			t.Fatalf("decode identity proof element %d: %v", index, err)
		}
		roundTrip, err := proof.Bytes()
		if err != nil {
			t.Fatalf("encode identity proof element %d: %v", index, err)
		}
		if !bytes.Equal(roundTrip[:], encoded) {
			t.Fatalf("identity proof element %d did not round trip", index)
		}
	}
}

func TestOpeningProofEnforcesResourcesBeforeDecoding(t *testing.T) {
	t.Parallel()

	_, fixture := readMultiProofFixture(t)
	tests := []struct {
		name     string
		limits   OpeningProofLimits
		resource OpeningProofResource
		limit    uint64
		actual   uint64
	}{
		{
			name: "proof bytes",
			limits: OpeningProofLimits{
				MaxProofBytes:    OpeningProofSize - 1,
				MaxPointDecodes:  openingProofPointCount,
				MaxScalarDecodes: 1,
			},
			resource: OpeningProofResourceBytes,
			limit:    OpeningProofSize - 1,
			actual:   OpeningProofSize,
		},
		{
			name: "points",
			limits: OpeningProofLimits{
				MaxProofBytes:    OpeningProofSize,
				MaxPointDecodes:  openingProofPointCount - 1,
				MaxScalarDecodes: 1,
			},
			resource: OpeningProofResourcePointDecodes,
			limit:    openingProofPointCount - 1,
			actual:   openingProofPointCount,
		},
		{
			name: "scalars",
			limits: OpeningProofLimits{
				MaxProofBytes:    OpeningProofSize,
				MaxPointDecodes:  openingProofPointCount,
				MaxScalarDecodes: 0,
			},
			resource: OpeningProofResourceScalarDecodes,
			limit:    0,
			actual:   1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := DecodeOpeningProof(context.Background(), fixture, test.limits)
			assertOpeningProofResourceError(
				t, err, test.resource, test.limit, test.actual,
			)
		})
	}
}

func TestOpeningProofRejectsInvalidLimitsStateAndContext(t *testing.T) {
	t.Parallel()

	_, fixture := readMultiProofFixture(t)
	invalid := []OpeningProofLimits{
		{},
		{MaxPointDecodes: 1, MaxScalarDecodes: 1},
		{MaxProofBytes: 1, MaxScalarDecodes: 1},
	}
	for _, limits := range invalid {
		if _, err := DecodeOpeningProof(
			context.Background(), fixture, limits,
		); !errors.Is(err, errInvalidOpeningProofLimits) {
			t.Fatalf("invalid limits error = %v, want %v", err, errInvalidOpeningProofLimits)
		}
	}

	var missingContext context.Context
	if _, err := DecodeOpeningProof(
		missingContext, fixture, testOpeningProofLimits(),
	); !errors.Is(err, errInvalidOpeningProofContext) {
		t.Fatalf("nil context error = %v, want %v", err, errInvalidOpeningProofContext)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DecodeOpeningProof(
		cancelled, fixture, testOpeningProofLimits(),
	); !errors.Is(err, errOpeningProofCancelled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context error = %v", err)
	}
	for cancelAt := 2; cancelAt <= openingProofPointCount+2; cancelAt++ {
		if _, err := DecodeOpeningProof(
			&commitCancelContext{cancelAt: cancelAt},
			fixture,
			testOpeningProofLimits(),
		); err != nil && !errors.Is(err, errOpeningProofCancelled) {
			t.Fatalf("cancel at %d error = %v", cancelAt, err)
		}
	}

	var zero OpeningProof
	if _, err := zero.Bytes(); !errors.Is(err, errInvalidOpeningProof) {
		t.Fatalf("zero proof error = %v, want %v", err, errInvalidOpeningProof)
	}
}

func TestOpeningProofValidatesEveryEncodedPoint(t *testing.T) {
	t.Parallel()

	_, fixture := readMultiProofFixture(t)
	for pointIndex := range openingProofPointCount {
		encoded := append([]byte(nil), fixture...)
		start := pointIndex * commitmentSize
		for index := start; index < start+commitmentSize; index++ {
			encoded[index] = 0xff
		}
		if _, err := DecodeOpeningProof(
			context.Background(), encoded, testOpeningProofLimits(),
		); !errors.Is(err, errInvalidOpeningProof) {
			t.Fatalf("corrupt point %d error = %v, want %v", pointIndex, err, errInvalidOpeningProof)
		}
	}
}

func assertOpeningProofResourceError(
	t testing.TB,
	err error,
	resource OpeningProofResource,
	limit uint64,
	actual uint64,
) {
	t.Helper()

	var resourceErr *OpeningProofResourceError
	if !errors.As(err, &resourceErr) {
		t.Fatalf("error = %v, want OpeningProofResourceError", err)
	}
	if resourceErr.Resource != resource || resourceErr.Limit != limit ||
		resourceErr.Actual != actual {
		t.Fatalf(
			"resource error = (%d, %d, %d), want (%d, %d, %d)",
			resourceErr.Resource,
			resourceErr.Limit,
			resourceErr.Actual,
			resource,
			limit,
			actual,
		)
	}
	if !errors.Is(err, errOpeningProofResource) || resourceErr.Error() == "" {
		t.Fatalf("resource error does not preserve sentinel: %v", err)
	}
}

func testOpeningProofLimits() OpeningProofLimits {
	return OpeningProofLimits{
		MaxProofBytes:    OpeningProofSize,
		MaxPointDecodes:  openingProofPointCount,
		MaxScalarDecodes: 1,
	}
}
