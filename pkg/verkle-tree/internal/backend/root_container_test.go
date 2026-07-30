package backend

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/crate-crypto/go-ipa/banderwagon"
	internalprofile "github.com/faustbrian/golib/pkg/verkle-tree/internal/profile"
)

func TestRootContainerCanonicalRoundTrip(t *testing.T) {
	t.Parallel()

	profile := internalprofile.ExperimentalBandersnatchIPA256V0()
	commitment := testNonIdentityCommitment(t)
	root, err := NewRoot(context.Background(), profile, commitment)
	if err != nil {
		t.Fatalf("new root: %v", err)
	}
	if empty, emptyErr := root.IsEmpty(); emptyErr != nil || empty {
		t.Fatalf("root empty = %t, error = %v", empty, emptyErr)
	}
	if got, profileErr := root.Profile(); profileErr != nil || got != profile {
		t.Fatalf("root profile = %#v, error = %v", got, profileErr)
	}

	encoded, err := root.Bytes()
	if err != nil {
		t.Fatalf("encode root: %v", err)
	}
	wantPrefix := []byte{'V', 'K', 'R', 'T', byte(profile.ID()), 0, 0, 0, 1, byte(RootKindCommitment)}
	if !bytes.Equal(encoded[:rootPayloadOffset], wantPrefix) {
		t.Fatalf("root header = %x, want %x", encoded[:rootPayloadOffset], wantPrefix)
	}
	wantCommitment, err := commitment.Bytes()
	if err != nil {
		t.Fatalf("encode commitment: %v", err)
	}
	if !bytes.Equal(encoded[rootPayloadOffset:], wantCommitment[:]) {
		t.Fatalf("root payload = %x, want %x", encoded[rootPayloadOffset:], wantCommitment)
	}

	input := bytes.Clone(encoded[:])
	decoded, err := DecodeRoot(context.Background(), input, testRootLimits())
	if err != nil {
		t.Fatalf("decode root: %v", err)
	}
	input[0] ^= 1
	reencoded, err := decoded.Bytes()
	if err != nil {
		t.Fatalf("re-encode root: %v", err)
	}
	if reencoded != encoded {
		t.Fatal("decoded root aliases caller bytes or changed canonical encoding")
	}
	payload, present, err := decoded.CommitmentBytes()
	if err != nil || !present || payload != wantCommitment {
		t.Fatalf("decoded commitment = %x, present = %t, error = %v", payload, present, err)
	}
	gotCommitment, err := decoded.Commitment()
	if err != nil {
		t.Fatalf("decoded root commitment: %v", err)
	}
	gotCommitmentBytes, err := gotCommitment.Bytes()
	if err != nil || gotCommitmentBytes != wantCommitment {
		t.Fatalf("decoded opaque commitment = %x, error = %v", gotCommitmentBytes, err)
	}
}

func TestRootContainerEncodesEmptyRootExplicitly(t *testing.T) {
	t.Parallel()

	root, err := NewRoot(
		context.Background(),
		internalprofile.ExperimentalBandersnatchIPA256V0(),
		testIdentityCommitment(),
	)
	if err != nil {
		t.Fatalf("new empty root: %v", err)
	}
	encoded, err := root.Bytes()
	if err != nil {
		t.Fatalf("encode empty root: %v", err)
	}
	if encoded[9] != byte(RootKindEmpty) ||
		!bytes.Equal(encoded[rootPayloadOffset:], make([]byte, commitmentSize)) {
		t.Fatalf("empty root encoding = %x", encoded)
	}
	limits := testRootLimits()
	limits.MaxPointDecodes = 0
	decoded, err := DecodeRoot(context.Background(), encoded[:], limits)
	if err != nil {
		t.Fatalf("decode empty root without point budget: %v", err)
	}
	if empty, emptyErr := decoded.IsEmpty(); emptyErr != nil || !empty {
		t.Fatalf("decoded root empty = %t, error = %v", empty, emptyErr)
	}
	payload, present, err := decoded.CommitmentBytes()
	if err != nil || present || payload != ([commitmentSize]byte{}) {
		t.Fatalf("empty payload = %x, present = %t, error = %v", payload, present, err)
	}
	commitment, err := decoded.Commitment()
	if err != nil {
		t.Fatalf("empty opaque commitment: %v", err)
	}
	if identity, identityErr := commitment.IsIdentity(); identityErr != nil || !identity {
		t.Fatalf("empty commitment identity = %t, error = %v", identity, identityErr)
	}
}

func TestRootContainerRejectsMalformedEncodings(t *testing.T) {
	t.Parallel()

	valid := testEncodedRoot(t)
	wrongSubgroup := mustDecodeRootHex(
		t,
		"280e608d5bbbe84b16aac62aa450e8921840ea563f1c9c266e0240d89cbe6a78",
	)
	tests := map[string][]byte{
		"empty":                  nil,
		"short":                  bytes.Clone(valid[:len(valid)-1]),
		"trailing byte":          append(bytes.Clone(valid), 0),
		"wrong magic":            mutateRootByte(valid, 0, 'X'),
		"wrong profile":          mutateRootByte(valid, 4, 2),
		"wrong profile version":  mutateRootByte(valid, 6, 1),
		"wrong encoding version": mutateRootByte(valid, 8, 2),
		"zero kind":              mutateRootByte(valid, 9, 0),
		"unknown kind":           mutateRootByte(valid, 9, 3),
		"identity commitment":    replaceRootPayload(valid, make([]byte, commitmentSize)),
		"non-canonical point":    replaceRootPayload(valid, bytes.Repeat([]byte{0xff}, commitmentSize)),
		"wrong-subgroup point":   replaceRootPayload(valid, wrongSubgroup),
	}
	emptyPayload := bytes.Clone(valid)
	emptyPayload[9] = byte(RootKindEmpty)
	emptyPayload[rootPayloadOffset] = 1
	tests["nonzero empty payload"] = emptyPayload

	for name, encoded := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			limits := testRootLimits()
			if len(encoded) > RootSize {
				limits.MaxRootBytes = RootSize + 1
			}
			_, err := DecodeRoot(context.Background(), encoded, limits)
			switch name {
			case "wrong profile", "wrong profile version", "wrong encoding version":
				if !errors.Is(err, internalprofile.ErrUnsupported) {
					t.Fatalf("decode error = %v, want ErrUnsupportedProfile", err)
				}
			default:
				if !errors.Is(err, errInvalidRoot) {
					t.Fatalf("decode error = %v, want %v", err, errInvalidRoot)
				}
			}
		})
	}
}

func TestRootContainerRejectsProfileBeforePointBudget(t *testing.T) {
	t.Parallel()

	encoded := testEncodedRoot(t)
	encoded[4] = 2
	limits := testRootLimits()
	limits.MaxPointDecodes = 0
	_, err := DecodeRoot(context.Background(), encoded, limits)
	if !errors.Is(err, internalprofile.ErrUnsupported) ||
		errors.Is(err, errRootResource) {
		t.Fatalf("decode error = %v, want profile mismatch before point budget", err)
	}
}

func TestRootContainerEnforcesResources(t *testing.T) {
	t.Parallel()

	encoded := testEncodedRoot(t)
	tests := []struct {
		name     string
		limits   RootLimits
		resource RootResource
		limit    uint64
		actual   uint64
	}{
		{
			name: "bytes",
			limits: RootLimits{
				MaxRootBytes:    RootSize - 1,
				MaxPointDecodes: 1,
			},
			resource: RootResourceBytes,
			limit:    RootSize - 1,
			actual:   RootSize,
		},
		{
			name: "point decodes",
			limits: RootLimits{
				MaxRootBytes:    RootSize,
				MaxPointDecodes: 0,
			},
			resource: RootResourcePointDecodes,
			limit:    0,
			actual:   1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := DecodeRoot(context.Background(), encoded, test.limits)
			var resourceErr *RootResourceError
			if !errors.As(err, &resourceErr) ||
				!errors.Is(err, errRootResource) ||
				resourceErr.Resource != test.resource ||
				resourceErr.Limit != test.limit ||
				resourceErr.Actual != test.actual ||
				resourceErr.Error() == "" {
				t.Fatalf("resource error = %v", err)
			}
		})
	}
}

func TestRootContainerRejectsInvalidStateContextAndLimits(t *testing.T) {
	t.Parallel()

	profile := internalprofile.ExperimentalBandersnatchIPA256V0()
	commitment := testNonIdentityCommitment(t)
	encoded := testEncodedRoot(t)
	if _, err := NewRoot(context.Background(), internalprofile.Profile{}, commitment); !errors.Is(
		err,
		internalprofile.ErrUnsupported,
	) {
		t.Fatalf("new root profile error = %v", err)
	}
	if _, err := NewRoot(
		context.Background(),
		profile,
		VectorCommitment{},
	); !errors.Is(err, errInvalidRoot) {
		t.Fatalf("new root commitment error = %v", err)
	}
	var missingContext context.Context
	if _, err := NewRoot(missingContext, profile, commitment); !errors.Is(err, errInvalidRootContext) {
		t.Fatalf("new root context error = %v", err)
	}
	if _, err := DecodeRoot(missingContext, encoded, testRootLimits()); !errors.Is(
		err,
		errInvalidRootContext,
	) {
		t.Fatalf("decode root context error = %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DecodeRoot(cancelled, encoded, testRootLimits()); !errors.Is(
		err,
		errRootCancelled,
	) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled root error = %v", err)
	}
	if _, err := DecodeRoot(context.Background(), encoded, RootLimits{}); !errors.Is(
		err,
		errInvalidRootLimits,
	) {
		t.Fatalf("invalid limits error = %v", err)
	}
	if _, err := NewRoot(
		&commitCancelContext{cancelAt: 2},
		profile,
		commitment,
	); !errors.Is(err, errRootCancelled) {
		t.Fatalf("new root cancellation error = %v", err)
	}
	for cancelAt := 2; cancelAt <= 3; cancelAt++ {
		if _, err := DecodeRoot(
			&commitCancelContext{cancelAt: cancelAt},
			encoded,
			testRootLimits(),
		); !errors.Is(err, errRootCancelled) {
			t.Fatalf("cancel at %d error = %v, want cancellation", cancelAt, err)
		}
	}

	var zero Root
	if _, err := zero.Bytes(); !errors.Is(err, errInvalidRoot) {
		t.Fatalf("zero root bytes error = %v", err)
	}
	if _, err := zero.Profile(); !errors.Is(err, errInvalidRoot) {
		t.Fatalf("zero root profile error = %v", err)
	}
	if _, err := zero.IsEmpty(); !errors.Is(err, errInvalidRoot) {
		t.Fatalf("zero root empty error = %v", err)
	}
	if _, _, err := zero.CommitmentBytes(); !errors.Is(err, errInvalidRoot) {
		t.Fatalf("zero root commitment error = %v", err)
	}

	corrupt := []Root{
		{
			profile: internalprofile.Profile{},
			kind:    RootKindEmpty,
			valid:   true,
		},
		{
			profile:    profile,
			kind:       RootKindEmpty,
			commitment: commitment,
			valid:      true,
		},
		{
			profile: profile,
			kind:    RootKindCommitment,
			valid:   true,
		},
		{
			profile:    profile,
			kind:       RootKindCommitment,
			commitment: testIdentityCommitment(),
			valid:      true,
		},
		{
			profile: profile,
			kind:    RootKind(255),
			valid:   true,
		},
	}
	for index, root := range corrupt {
		if _, err := root.Bytes(); !errors.Is(err, errInvalidRoot) {
			t.Fatalf("corrupt root %d error = %v", index, err)
		}
	}
}

func TestInvalidRootRejectsCommitmentAccess(t *testing.T) {
	t.Parallel()

	if _, err := (Root{}).Commitment(); !errors.Is(err, errInvalidRoot) {
		t.Fatalf("invalid root commitment error = %v", err)
	}
}

func testEncodedRoot(t testing.TB) []byte {
	t.Helper()

	root, err := NewRoot(
		context.Background(),
		internalprofile.ExperimentalBandersnatchIPA256V0(),
		testNonIdentityCommitment(t),
	)
	if err != nil {
		t.Fatalf("new test root: %v", err)
	}
	encoded, err := root.Bytes()
	if err != nil {
		t.Fatalf("encode test root: %v", err)
	}

	return bytes.Clone(encoded[:])
}

func testNonIdentityCommitment(t testing.TB) VectorCommitment {
	t.Helper()

	encoded := mustDecodeRootHex(
		t,
		"4a2c7486fd924882bf02c6908de395122843e3e05264d7991e18e7985dad51e9",
	)
	value, err := decodeCommitment(encoded)
	if err != nil {
		t.Fatalf("decode test commitment: %v", err)
	}

	return VectorCommitment{value: value, valid: true}
}

func testIdentityCommitment() VectorCommitment {
	var identity banderwagon.Element
	identity.SetIdentity()

	return VectorCommitment{
		value: commitment{element: identity},
		valid: true,
	}
}

func mustDecodeRootHex(t testing.TB, encoded string) []byte {
	t.Helper()

	decoded, err := hex.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode root fixture: %v", err)
	}

	return decoded
}

func mutateRootByte(encoded []byte, index int, value byte) []byte {
	mutated := bytes.Clone(encoded)
	mutated[index] = value

	return mutated
}

func replaceRootPayload(encoded, payload []byte) []byte {
	mutated := bytes.Clone(encoded)
	copy(mutated[rootPayloadOffset:], payload)

	return mutated
}

func testRootLimits() RootLimits {
	return RootLimits{
		MaxRootBytes:    RootSize,
		MaxPointDecodes: 1,
	}
}

func testProfile() internalprofile.Profile {
	return internalprofile.ExperimentalBandersnatchIPA256V0()
}
