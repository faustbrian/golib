package authstate

import (
	"context"
	"errors"
	"sync"
	"testing"

	verkletree "github.com/faustbrian/golib/pkg/verkle-tree"
)

func TestClaimSetCanonicalizesAndOwnsClaims(t *testing.T) {
	t.Parallel()

	key1 := testKey(1, 1)
	key2 := testKey(2, 2)
	key3 := testKey(3, 3)
	input := []Claim{
		Absence(key3),
		Membership(key1, Value{}),
		Membership(key2, testValue(2)),
	}
	set, err := NewClaimSet(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		input,
		testClaimLimits(),
	)
	if err != nil {
		t.Fatalf("new claim set: %v", err)
	}
	input[0] = Membership(testKey(9, 9), testValue(9))

	if count, countErr := set.Count(); countErr != nil || count != 3 {
		t.Fatalf("claim count = %d, error = %v", count, countErr)
	}
	if profile, profileErr := set.Profile(); profileErr != nil ||
		profile != verkletree.ExperimentalBandersnatchIPA256V0() {
		t.Fatalf("claim profile = %#v, error = %v", profile, profileErr)
	}
	claims, err := set.Claims(context.Background())
	if err != nil {
		t.Fatalf("get claims: %v", err)
	}
	if got := claimKey(t, claims[0]); got != key1 {
		t.Fatalf("claim 0 key = %x, want %x", got, key1)
	}
	if got := claimKey(t, claims[1]); got != key2 {
		t.Fatalf("claim 1 key = %x, want %x", got, key2)
	}
	if got := claimKey(t, claims[2]); got != key3 {
		t.Fatalf("claim 2 key = %x, want %x", got, key3)
	}
	value, present, err := claims[0].Value()
	if err != nil || !present || value != (Value{}) {
		t.Fatalf("present-zero claim = %x/%t, error = %v", value, present, err)
	}
	value, present, err = claims[2].Value()
	if err != nil || present || value != (Value{}) {
		t.Fatalf("absence claim = %x/%t, error = %v", value, present, err)
	}
	claims[0] = Absence(key1)
	repeated, err := set.Claims(context.Background())
	if err != nil {
		t.Fatalf("get repeated claims: %v", err)
	}
	if kind, kindErr := repeated[0].Kind(); kindErr != nil ||
		kind != ClaimMembership {
		t.Fatalf("retained claim kind = %d, error = %v", kind, kindErr)
	}
	for _, test := range []struct {
		key       Key
		wantKind  ClaimKind
		wantFound bool
	}{
		{key: key1, wantKind: ClaimMembership, wantFound: true},
		{key: key3, wantKind: ClaimAbsence, wantFound: true},
		{key: testKey(8, 8), wantFound: false},
	} {
		claim, found, lookupErr := set.Lookup(test.key)
		if lookupErr != nil || found != test.wantFound {
			t.Fatalf("lookup %x found = %t, error = %v", test.key, found, lookupErr)
		}
		if found {
			kind, kindErr := claim.Kind()
			if kindErr != nil || kind != test.wantKind {
				t.Fatalf("lookup %x kind = %d, error = %v", test.key, kind, kindErr)
			}
		}
	}
}

func TestClaimSetRejectsDuplicatesAndInvalidClaims(t *testing.T) {
	t.Parallel()

	key := testKey(1, 1)
	if _, err := NewClaimSet(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		nil,
		testClaimLimits(),
	); !errors.Is(err, errInvalidClaimSet) {
		t.Fatalf("empty claim set error = %v, want %v", err, errInvalidClaimSet)
	}
	tests := map[string][]Claim{
		"duplicate membership": {
			Membership(key, testValue(1)),
			Membership(key, testValue(2)),
		},
		"conflicting duplicate": {
			Membership(key, testValue(1)),
			Absence(key),
		},
		"zero claim": {{}},
		"absence with value": {{
			kind:  ClaimAbsence,
			key:   key,
			value: testValue(1),
		}},
		"unknown kind": {{
			kind: ClaimKind(255),
			key:  key,
		}},
	}
	for name, claims := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := NewClaimSet(
				context.Background(),
				verkletree.ExperimentalBandersnatchIPA256V0(),
				claims,
				testClaimLimits(),
			)
			switch name {
			case "duplicate membership", "conflicting duplicate":
				if !errors.Is(err, errDuplicateClaimKey) {
					t.Fatalf("claim error = %v, want %v", err, errDuplicateClaimKey)
				}
			default:
				if !errors.Is(err, errInvalidClaim) {
					t.Fatalf("claim error = %v, want %v", err, errInvalidClaim)
				}
			}
		})
	}
}

func TestClaimSetRejectsProfileBeforeResources(t *testing.T) {
	t.Parallel()

	_, err := NewClaimSet(
		context.Background(),
		verkletree.Profile{},
		[]Claim{Membership(testKey(1, 1), testValue(1))},
		ClaimLimits{MaxClaims: 1, MaxTemporaryBytes: 1},
	)
	if !errors.Is(err, verkletree.ErrUnsupportedProfile) ||
		errors.Is(err, errClaimResource) {
		t.Fatalf("claim error = %v, want profile mismatch before resources", err)
	}
}

func TestClaimSetEnforcesResourcesBeforeAllocation(t *testing.T) {
	t.Parallel()

	claims := []Claim{
		Membership(testKey(1, 1), testValue(1)),
		Absence(testKey(2, 2)),
	}
	tests := []struct {
		name     string
		limits   ClaimLimits
		resource ClaimResource
		limit    uint64
		actual   uint64
	}{
		{
			name: "claim count",
			limits: ClaimLimits{
				MaxClaims:         1,
				MaxTemporaryBytes: 2 * 2 * claimWorkingBytes,
			},
			resource: ClaimResourceClaims,
			limit:    1,
			actual:   2,
		},
		{
			name: "temporary bytes",
			limits: ClaimLimits{
				MaxClaims:         2,
				MaxTemporaryBytes: 2*2*claimWorkingBytes - 1,
			},
			resource: ClaimResourceTemporaryBytes,
			limit:    2*2*claimWorkingBytes - 1,
			actual:   2 * 2 * claimWorkingBytes,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewClaimSet(
				context.Background(),
				verkletree.ExperimentalBandersnatchIPA256V0(),
				claims,
				test.limits,
			)
			var resourceErr *ClaimResourceError
			if !errors.As(err, &resourceErr) ||
				!errors.Is(err, errClaimResource) ||
				resourceErr.Resource != test.resource ||
				resourceErr.Limit != test.limit ||
				resourceErr.Actual != test.actual ||
				resourceErr.Error() == "" {
				t.Fatalf("resource error = %v", err)
			}
		})
	}
}

func TestClaimSetRejectsInvalidStateContextAndLimits(t *testing.T) {
	t.Parallel()

	profile := verkletree.ExperimentalBandersnatchIPA256V0()
	claims := []Claim{
		Membership(testKey(2, 2), testValue(2)),
		Absence(testKey(1, 1)),
	}
	var missingContext context.Context
	if _, err := NewClaimSet(
		missingContext,
		profile,
		claims,
		testClaimLimits(),
	); !errors.Is(err, errInvalidClaimContext) {
		t.Fatalf("nil context error = %v", err)
	}
	invalidLimits := []ClaimLimits{
		{},
		{MaxClaims: 1},
		{MaxTemporaryBytes: 1},
		{MaxClaims: maxClaimCount + 1, MaxTemporaryBytes: 1},
	}
	for _, limits := range invalidLimits {
		if _, err := NewClaimSet(
			context.Background(),
			profile,
			claims,
			limits,
		); !errors.Is(err, errInvalidClaimLimits) {
			t.Fatalf("invalid limits %#v error = %v", limits, err)
		}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewClaimSet(
		cancelled,
		profile,
		claims,
		testClaimLimits(),
	); !errors.Is(err, errClaimCancelled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled claim set error = %v", err)
	}
	for successful := 0; successful <= 30; successful++ {
		_, err := NewClaimSet(
			&stepContext{successfulChecks: successful},
			profile,
			claims,
			testClaimLimits(),
		)
		if err != nil && !errors.Is(err, errClaimCancelled) {
			t.Fatalf("claim cancellation after %d checks = %v", successful, err)
		}
	}
	single, err := NewClaimSet(
		context.Background(),
		profile,
		[]Claim{Absence(testKey(1, 1))},
		testClaimLimits(),
	)
	if err != nil {
		t.Fatalf("single claim set: %v", err)
	}
	var missingReadContext context.Context
	if _, err := single.Claims(missingReadContext); !errors.Is(err, errInvalidClaimContext) {
		t.Fatalf("nil claim-read context error = %v", err)
	}
	for successful := 0; successful <= 3; successful++ {
		_, err := single.Claims(&stepContext{successfulChecks: successful})
		if err != nil && !errors.Is(err, errClaimCancelled) {
			t.Fatalf("claim read cancellation after %d checks = %v", successful, err)
		}
	}

	var zeroSet ClaimSet
	if _, err := zeroSet.Count(); !errors.Is(err, errInvalidClaimSet) {
		t.Fatalf("zero set count error = %v", err)
	}
	if _, err := zeroSet.Profile(); !errors.Is(err, errInvalidClaimSet) {
		t.Fatalf("zero set profile error = %v", err)
	}
	if _, err := zeroSet.Claims(context.Background()); !errors.Is(err, errInvalidClaimSet) {
		t.Fatalf("zero set claims error = %v", err)
	}
	if _, _, err := zeroSet.Lookup(Key{}); !errors.Is(err, errInvalidClaimSet) {
		t.Fatalf("zero set lookup error = %v", err)
	}
	var zeroClaim Claim
	if _, err := zeroClaim.Kind(); !errors.Is(err, errInvalidClaim) {
		t.Fatalf("zero claim kind error = %v", err)
	}
	if _, err := zeroClaim.Key(); !errors.Is(err, errInvalidClaim) {
		t.Fatalf("zero claim key error = %v", err)
	}
	if _, _, err := zeroClaim.Value(); !errors.Is(err, errInvalidClaim) {
		t.Fatalf("zero claim value error = %v", err)
	}
}

func TestClaimLimitsAndSetValidationHonorImplementationBoundaries(t *testing.T) {
	t.Parallel()

	if err := (ClaimLimits{
		MaxClaims:         maxClaimCount,
		MaxTemporaryBytes: 1,
	}).validate(); err != nil {
		t.Fatalf("maximum claim limit: %v", err)
	}

	profile := verkletree.ExperimentalBandersnatchIPA256V0()
	exactMaximum := ClaimSet{
		profile: profile,
		claims:  make([]Claim, maxClaimCount),
		valid:   true,
	}
	if count, err := exactMaximum.Count(); err != nil || count != maxClaimCount {
		t.Fatalf("maximum claim set count = %d, error = %v", count, err)
	}

	tests := map[string]ClaimSet{
		"missing validity marker": {
			profile: profile,
			claims:  []Claim{Absence(testKey(1, 1))},
		},
		"invalid profile": {
			claims: []Claim{Absence(testKey(1, 1))},
			valid:  true,
		},
		"excessive retained claims": {
			profile: profile,
			claims:  make([]Claim, int(maxClaimCount)+1),
			valid:   true,
		},
	}
	for name, set := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := set.Count(); !errors.Is(err, errInvalidClaimSet) {
				t.Fatalf("corrupt claim set error = %v, want %v", err, errInvalidClaimSet)
			}
		})
	}
}

func TestClaimSetCanonicalizesMergeBoundaries(t *testing.T) {
	t.Parallel()

	for name, order := range map[string][]byte{
		"two reversed":       {2, 1},
		"four reversed":      {4, 3, 2, 1},
		"left drains first":  {1, 2, 4, 3},
		"right drains first": {2, 4, 1, 3},
		"uneven left drains": {1, 2, 3, 4, 5},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			claims := make([]Claim, len(order))
			for index, keyByte := range order {
				claims[index] = Absence(testKey(keyByte, keyByte))
			}
			set, err := NewClaimSet(
				context.Background(),
				verkletree.ExperimentalBandersnatchIPA256V0(),
				claims,
				testClaimLimits(),
			)
			if err != nil {
				t.Fatalf("new claim set: %v", err)
			}
			canonical, err := set.Claims(context.Background())
			if err != nil {
				t.Fatalf("get claims: %v", err)
			}
			for index, claim := range canonical {
				want := testKey(byte(index+1), byte(index+1))
				if got := claimKey(t, claim); got != want {
					t.Fatalf("claim %d key = %x, want %x", index, got, want)
				}
			}
		})
	}
}

func TestClaimSetSupportsConcurrentImmutableReads(t *testing.T) {
	t.Parallel()

	set, err := NewClaimSet(
		context.Background(),
		verkletree.ExperimentalBandersnatchIPA256V0(),
		[]Claim{
			Membership(testKey(1, 1), testValue(1)),
			Absence(testKey(2, 2)),
		},
		testClaimLimits(),
	)
	if err != nil {
		t.Fatalf("new claim set: %v", err)
	}
	const workers = 16
	var group sync.WaitGroup
	group.Add(workers)
	failures := make(chan error, workers)
	for range workers {
		go func() {
			defer group.Done()
			claims, readErr := set.Claims(context.Background())
			if readErr != nil {
				failures <- readErr
				return
			}
			if len(claims) != 2 {
				failures <- errInvalidClaimSet
			}
		}()
	}
	group.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
}

func claimKey(t testing.TB, claim Claim) Key {
	t.Helper()

	key, err := claim.Key()
	if err != nil {
		t.Fatalf("claim key: %v", err)
	}

	return key
}

func testClaimLimits() ClaimLimits {
	return ClaimLimits{
		MaxClaims:         16,
		MaxTemporaryBytes: 16 * 2 * claimWorkingBytes,
	}
}
