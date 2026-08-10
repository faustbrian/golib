package httpsignature

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

type resolverFunc func(context.Context, string) (ResolvedKey, error)

func (resolve resolverFunc) Resolve(ctx context.Context, keyID string) (ResolvedKey, error) {
	return resolve(ctx, keyID)
}

func TestVerifierEnforcesProfileThenAtomicallyConsumesReplayRecord(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, err := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewHMACKey() error = %v", err)
	}
	replay, err := NewMemoryReplayStore(MemoryReplayConfig{
		Capacity: 4,
		MaxTTL:   10 * time.Minute,
		Now:      func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewMemoryReplayStore() error = %v", err)
	}
	profile, err := NewVerificationProfile(VerificationProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		RequiredComponents: []ComponentIdentifier{
			{Name: "@method"},
			{Name: "@authority"},
		},
		Created:            ParameterRequired,
		Expires:            ParameterRequired,
		AlgorithmParameter: ParameterRequired,
		Nonce:              ParameterRequired,
		Tag:                ParameterRequired,
		AllowedTags:        []string{"payment"},
		MaxAge:             5 * time.Minute,
		ClockSkew:          10 * time.Second,
		ResolveTimeout:     time.Second,
		Now:                func() time.Time { return now },
		Resolver: resolverFunc(func(_ context.Context, keyID string) (ResolvedKey, error) {
			if keyID != "key-1" {
				return ResolvedKey{}, ErrKeyNotFound
			}
			return ResolvedKey{
				Algorithm:  HMACSHA256,
				Key:        key,
				NotBefore:  now.Add(-time.Hour),
				NotAfter:   now.Add(time.Hour),
				FreshUntil: now.Add(30 * time.Minute),
			}, nil
		}),
		Replay: replay,
	})
	if err != nil {
		t.Fatalf("NewVerificationProfile() error = %v", err)
	}

	request, err := http.NewRequest(http.MethodPost, "https://example.com/pay", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	inputs, err := ParseSignatureInputs([]string{
		`sig=("@method" "@authority");created=1700000000;expires=1700000060;nonce="nonce-1";keyid="key-1";alg="hmac-sha256";tag="payment"`,
	})
	if err != nil {
		t.Fatalf("ParseSignatureInputs() error = %v", err)
	}
	input := inputs.Entries()[0]
	base, err := CreateSignatureBase(MessageContext{Request: request}, input)
	if err != nil {
		t.Fatalf("CreateSignatureBase() error = %v", err)
	}
	value, err := Sign(context.Background(), HMACSHA256, key, []byte(base), nil)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	signatures := Signatures{entries: []SignatureValue{{Label: "sig", Value: value}}}

	verifier := NewVerifier(profile)
	verified, err := verifier.Verify(context.Background(), MessageContext{Request: request}, "sig", inputs, signatures)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if verified.Label != "sig" || verified.KeyID != "key-1" || verified.Algorithm != HMACSHA256 {
		t.Fatalf("Verify() = %#v", verified)
	}
	if _, err := verifier.Verify(context.Background(), MessageContext{Request: request}, "sig", inputs, signatures); !verificationFailureIs(err, VerificationReplay) {
		t.Fatalf("second Verify() error = %v, want replay failure", err)
	}
}

func TestVerifierDoesNotConsumeNonceForInvalidCryptographicSignature(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	profile, replay := testVerificationProfile(t, now, key)
	request, _ := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	inputs, _ := ParseSignatureInputs([]string{
		`sig=("@method");created=1700000000;expires=1700000060;nonce="nonce";keyid="key";alg="hmac-sha256"`,
	})
	bad := Signatures{entries: []SignatureValue{{Label: "sig", Value: []byte("bad")}}}
	if _, err := NewVerifier(profile).Verify(context.Background(), MessageContext{Request: request}, "sig", inputs, bad); !verificationFailureIs(err, VerificationCryptographic) {
		t.Fatalf("Verify(bad) error = %v, want cryptographic failure", err)
	}

	input := inputs.Entries()[0]
	base, _ := CreateSignatureBase(MessageContext{Request: request}, input)
	value, _ := Sign(context.Background(), HMACSHA256, key, []byte(base), nil)
	good := Signatures{entries: []SignatureValue{{Label: "sig", Value: value}}}
	if _, err := NewVerifier(profile).Verify(context.Background(), MessageContext{Request: request}, "sig", inputs, good); err != nil {
		t.Fatalf("Verify(good) error = %v", err)
	}
	if err := replay.Consume(context.Background(), ReplayRecord{KeyID: "key", Nonce: "nonce", ExpiresAt: now.Add(time.Minute)}); !errors.Is(err, ErrReplayDetected) {
		t.Fatalf("replay state error = %v, want ErrReplayDetected", err)
	}
}

func TestVerifierRejectsDifferentLabelSetsBeforeSelection(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	profile, _ := testVerificationProfile(t, now, key)
	inputs, err := ParseSignatureInputs([]string{
		`sig=("@method");created=1700000000;expires=1700000060;nonce="nonce";keyid="key";alg="hmac-sha256", extra=("@method")`,
	})
	if err != nil {
		t.Fatalf("ParseSignatureInputs() error = %v", err)
	}
	signatures := Signatures{entries: []SignatureValue{{Label: "sig", Value: []byte("value")}}}

	_, err = NewVerifier(profile).Verify(context.Background(), MessageContext{}, "sig", inputs, signatures)
	if !verificationFailureIs(err, VerificationSelection) {
		t.Fatalf("Verify() error = %v, want selection failure", err)
	}
}

func TestVerificationProfileMatchesRequiredComponentParametersIndependentOfOrder(t *testing.T) {
	t.Parallel()

	profile, err := NewVerificationProfile(VerificationProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		RequiredComponents: []ComponentIdentifier{{
			Name: "example",
			Parameters: []Parameter{
				{Name: "req", Value: true},
				{Name: "sf", Value: true},
			},
		}},
		Created:            ParameterForbidden,
		Expires:            ParameterForbidden,
		AlgorithmParameter: ParameterForbidden,
		Nonce:              ParameterForbidden,
		Tag:                ParameterForbidden,
		ResolveTimeout:     time.Second,
		Now:                time.Now,
		Resolver: resolverFunc(func(context.Context, string) (ResolvedKey, error) {
			return ResolvedKey{}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewVerificationProfile() error = %v", err)
	}
	inputs, err := ParseSignatureInputs([]string{`sig=("example";sf;req);keyid="key"`})
	if err != nil {
		t.Fatalf("ParseSignatureInputs() error = %v", err)
	}
	if _, err := profile.validateInput(inputs.Entries()[0]); err != nil {
		t.Fatalf("validateInput() error = %v", err)
	}
}

func TestVerifierRetainsNonceThroughAcceptedExpirationSkew(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	replay, err := NewMemoryReplayStore(MemoryReplayConfig{Capacity: 1, MaxTTL: time.Minute, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewMemoryReplayStore() error = %v", err)
	}
	profile, err := NewVerificationProfile(VerificationProfileConfig{
		AllowedAlgorithms:  []Algorithm{HMACSHA256},
		RequiredComponents: []ComponentIdentifier{{Name: "@method"}},
		Created:            ParameterRequired,
		Expires:            ParameterRequired,
		AlgorithmParameter: ParameterRequired,
		Nonce:              ParameterRequired,
		Tag:                ParameterForbidden,
		MaxAge:             time.Minute,
		ClockSkew:          10 * time.Second,
		ResolveTimeout:     time.Second,
		Now:                func() time.Time { return now },
		Resolver: resolverFunc(func(context.Context, string) (ResolvedKey, error) {
			return ResolvedKey{Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Minute), FreshUntil: now.Add(time.Minute)}, nil
		}),
		Replay: replay,
	})
	if err != nil {
		t.Fatalf("NewVerificationProfile() error = %v", err)
	}
	request, _ := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	inputs, _ := ParseSignatureInputs([]string{
		`sig=("@method");created=1699999970;expires=1699999995;nonce="nonce";keyid="key";alg="hmac-sha256"`,
	})
	base, _ := CreateSignatureBase(MessageContext{Request: request}, inputs.Entries()[0])
	value, _ := Sign(context.Background(), HMACSHA256, key, []byte(base), nil)
	signatures := Signatures{entries: []SignatureValue{{Label: "sig", Value: value}}}

	if _, err := NewVerifier(profile).Verify(context.Background(), MessageContext{Request: request}, "sig", inputs, signatures); err != nil {
		t.Fatalf("Verify() within expiration skew error = %v", err)
	}
}

func TestVerificationProfileRejectsZeroLengthKeyValidityWindow(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	profile, _ := testVerificationProfile(t, now, key)
	err := profile.validateKey(ResolvedKey{
		Algorithm:  HMACSHA256,
		Key:        key,
		NotBefore:  now,
		NotAfter:   now,
		FreshUntil: now.Add(time.Minute),
	}, HMACSHA256, true)
	if !verificationFailureIs(err, VerificationKey) {
		t.Fatalf("validateKey() error = %v, want key failure", err)
	}
}

func TestVerificationProfileRejectsImplicitOrIncoherentPolicy(t *testing.T) {
	t.Parallel()

	base := VerificationProfileConfig{
		AllowedAlgorithms:  []Algorithm{HMACSHA256},
		RequiredComponents: []ComponentIdentifier{{Name: "@method"}},
		Created:            ParameterRequired,
		Expires:            ParameterOptional,
		AlgorithmParameter: ParameterRequired,
		Nonce:              ParameterForbidden,
		Tag:                ParameterForbidden,
		MaxAge:             time.Minute,
		ClockSkew:          time.Second,
		ResolveTimeout:     time.Second,
		Now:                time.Now,
		Resolver:           resolverFunc(func(context.Context, string) (ResolvedKey, error) { return ResolvedKey{}, nil }),
	}

	invalid := []VerificationProfileConfig{
		{},
		func() VerificationProfileConfig { value := base; value.AllowedAlgorithms = nil; return value }(),
		func() VerificationProfileConfig { value := base; value.RequiredComponents = nil; return value }(),
		func() VerificationProfileConfig { value := base; value.Created = 0; return value }(),
		func() VerificationProfileConfig { value := base; value.Nonce = ParameterRequired; return value }(),
		func() VerificationProfileConfig { value := base; value.Tag = ParameterRequired; return value }(),
		func() VerificationProfileConfig {
			value := base
			value.Tag = ParameterRequired
			value.AllowedTags = []string{"bad\nvalue"}
			return value
		}(),
		func() VerificationProfileConfig { value := base; value.ResolveTimeout = 0; return value }(),
		func() VerificationProfileConfig {
			value := base
			value.MaxAge = time.Duration(1<<63 - 1)
			value.ClockSkew = time.Nanosecond
			return value
		}(),
	}
	for _, config := range invalid {
		if _, err := NewVerificationProfile(config); !errors.Is(err, ErrInvalidVerificationProfile) {
			t.Fatalf("NewVerificationProfile() error = %v, want ErrInvalidVerificationProfile", err)
		}
	}
}

func TestVerifierReturnsSafeTypedFailuresForPolicyTimeAndKeyErrors(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	profile, _ := testVerificationProfile(t, now, key)
	request, _ := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	signatures := Signatures{entries: []SignatureValue{{Label: "sig", Value: []byte("irrelevant")}}}

	tests := []struct {
		name  string
		field string
		want  VerificationFailure
	}{
		{name: "insufficient coverage", field: `sig=("@path");created=1700000000;expires=1700000060;nonce="secret-nonce";keyid="secret-key";alg="hmac-sha256"`, want: VerificationPolicy},
		{name: "future", field: `sig=("@method");created=1700000002;expires=1700000060;nonce="secret-nonce";keyid="secret-key";alg="hmac-sha256"`, want: VerificationTime},
		{name: "expired", field: `sig=("@method");created=1699999900;expires=1699999999;nonce="secret-nonce";keyid="secret-key";alg="hmac-sha256"`, want: VerificationTime},
		{name: "wrong algorithm", field: `sig=("@method");created=1700000000;expires=1700000060;nonce="secret-nonce";keyid="secret-key";alg="ed25519"`, want: VerificationAlgorithm},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inputs, err := ParseSignatureInputs([]string{test.field})
			if err != nil {
				t.Fatalf("ParseSignatureInputs() error = %v", err)
			}
			_, err = NewVerifier(profile).Verify(context.Background(), MessageContext{Request: request}, "sig", inputs, signatures)
			if !verificationFailureIs(err, test.want) {
				t.Fatalf("Verify() error = %v, want %s", err, test.want)
			}
			if strings.Contains(err.Error(), "secret-key") || strings.Contains(err.Error(), "secret-nonce") || strings.Contains(err.Error(), "@method") {
				t.Fatalf("Verify() error discloses sensitive input: %q", err)
			}
		})
	}
}

func TestVerifierSanitizesExternalResolverErrorsThroughoutErrorChain(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	profile, err := NewVerificationProfile(VerificationProfileConfig{
		AllowedAlgorithms:  []Algorithm{HMACSHA256},
		RequiredComponents: []ComponentIdentifier{{Name: "@method"}},
		Created:            ParameterRequired,
		Expires:            ParameterOptional,
		AlgorithmParameter: ParameterRequired,
		Nonce:              ParameterForbidden,
		Tag:                ParameterForbidden,
		MaxAge:             time.Minute,
		ClockSkew:          time.Second,
		ResolveTimeout:     time.Second,
		Now:                func() time.Time { return now },
		Resolver: resolverFunc(func(context.Context, string) (ResolvedKey, error) {
			return ResolvedKey{}, errors.New("backend lookup exposed secret-key")
		}),
	})
	if err != nil {
		t.Fatalf("NewVerificationProfile() error = %v", err)
	}
	inputs, _ := ParseSignatureInputs([]string{`sig=("@method");created=1700000000;keyid="secret-key";alg="hmac-sha256"`})
	signatures := Signatures{entries: []SignatureValue{{Label: "sig", Value: []byte("value")}}}
	request, _ := http.NewRequest(http.MethodGet, "https://example.com/", nil)

	_, err = NewVerifier(profile).Verify(context.Background(), MessageContext{Request: request}, "sig", inputs, signatures)
	if !verificationFailureIs(err, VerificationKeyResolution) || !errors.Is(err, ErrKeyResolutionFailure) {
		t.Fatalf("Verify() error = %v, want safe key-resolution failure", err)
	}
	for current := err; current != nil; current = errors.Unwrap(current) {
		if strings.Contains(current.Error(), "secret-key") {
			t.Fatalf("error chain discloses resolver detail: %q", current)
		}
	}
}

func TestVerifierRejectsResolverSuccessAfterItsDeadline(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	profile, err := NewVerificationProfile(VerificationProfileConfig{
		AllowedAlgorithms:  []Algorithm{HMACSHA256},
		RequiredComponents: []ComponentIdentifier{{Name: "@method"}},
		Created:            ParameterRequired,
		Expires:            ParameterOptional,
		AlgorithmParameter: ParameterRequired,
		Nonce:              ParameterForbidden,
		Tag:                ParameterForbidden,
		MaxAge:             time.Minute,
		ResolveTimeout:     time.Millisecond,
		Now:                func() time.Time { return now },
		Resolver: resolverFunc(func(ctx context.Context, _ string) (ResolvedKey, error) {
			<-ctx.Done()
			return ResolvedKey{Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Minute), FreshUntil: now.Add(time.Minute)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewVerificationProfile() error = %v", err)
	}
	request, _ := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	inputs, _ := ParseSignatureInputs([]string{`sig=("@method");created=1700000000;keyid="key";alg="hmac-sha256"`})
	base, _ := CreateSignatureBase(MessageContext{Request: request}, inputs.Entries()[0])
	value, _ := Sign(context.Background(), HMACSHA256, key, []byte(base), nil)
	signatures := Signatures{entries: []SignatureValue{{Label: "sig", Value: value}}}

	_, err = NewVerifier(profile).Verify(context.Background(), MessageContext{Request: request}, "sig", inputs, signatures)
	if !verificationFailureIs(err, VerificationKeyResolution) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Verify() error = %v, want key-resolution deadline", err)
	}
}

func testVerificationProfile(t *testing.T, now time.Time, key HMACKey) (*VerificationProfile, *MemoryReplayStore) {
	t.Helper()

	replay, err := NewMemoryReplayStore(MemoryReplayConfig{Capacity: 4, MaxTTL: 2 * time.Minute, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewMemoryReplayStore() error = %v", err)
	}
	profile, err := NewVerificationProfile(VerificationProfileConfig{
		AllowedAlgorithms:  []Algorithm{HMACSHA256},
		RequiredComponents: []ComponentIdentifier{{Name: "@method"}},
		Created:            ParameterRequired,
		Expires:            ParameterRequired,
		AlgorithmParameter: ParameterRequired,
		Nonce:              ParameterRequired,
		Tag:                ParameterForbidden,
		MaxAge:             time.Minute,
		ClockSkew:          time.Second,
		ResolveTimeout:     time.Second,
		Now:                func() time.Time { return now },
		Resolver: resolverFunc(func(_ context.Context, keyID string) (ResolvedKey, error) {
			if keyID != "key" {
				return ResolvedKey{}, ErrKeyNotFound
			}
			return ResolvedKey{
				Algorithm:  HMACSHA256,
				Key:        key,
				NotBefore:  now.Add(-time.Minute),
				NotAfter:   now.Add(time.Minute),
				FreshUntil: now.Add(30 * time.Second),
			}, nil
		}),
		Replay: replay,
	})
	if err != nil {
		t.Fatalf("NewVerificationProfile() error = %v", err)
	}

	return profile, replay
}

func verificationFailureIs(err error, want VerificationFailure) bool {
	var verificationError *VerificationError
	return errors.As(err, &verificationError) && verificationError.Failure == want
}
