package httpsignature

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestSigningProfileAndSignedFieldBoundarySemantics(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	base := func() SigningProfileConfig {
		return SigningProfileConfig{
			AllowedAlgorithms: []Algorithm{HMACSHA256}, CoveredComponents: []ComponentIdentifier{{Name: "@method"}},
			Expires: ParameterRequired, AlgorithmParameter: ParameterRequired, Nonce: ParameterForbidden, Tag: ParameterForbidden,
			Lifetime: time.Minute, ResolveTimeout: time.Second, Now: func() time.Time { return now },
			Provider: signingKeyProviderFunc(func(context.Context) (SigningKey, error) {
				return SigningKey{KeyID: "key", Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}, nil
			}),
		}
	}
	for _, mutate := range []func(*SigningProfileConfig){
		func(config *SigningProfileConfig) { config.AllowedAlgorithms = []Algorithm{"unsupported"} },
		func(config *SigningProfileConfig) { config.AllowedAlgorithms = []Algorithm{HMACSHA256, HMACSHA256} },
		func(config *SigningProfileConfig) { config.AllowedAlgorithms = []Algorithm{RSAPSSSHA512} },
		func(config *SigningProfileConfig) {
			config.CoveredComponents = []ComponentIdentifier{{Name: "@method"}, {Name: "@method"}}
		},
	} {
		config := base()
		mutate(&config)
		if _, err := NewSigningProfile(config); !errors.Is(err, ErrInvalidSigningProfile) {
			t.Fatalf("invalid signing config error = %v", err)
		}
	}
	if _, _, err := CombineSignedFields(); !errors.Is(err, ErrInvalidSignedFields) {
		t.Fatalf("empty CombineSignedFields error = %v", err)
	}
	if _, _, err := CombineSignedFields(SignedFields{input: SignatureInput{Label: "sig"}, signature: SignatureValue{Label: "other", Value: []byte("x")}}); !errors.Is(err, ErrInvalidSignedFields) {
		t.Fatalf("mismatched fields error = %v", err)
	}
	if got := (SignedFields{signature: SignatureValue{Label: "Bad", Value: []byte("x")}}).SignatureField(); got != "" {
		t.Fatalf("invalid-label SignatureField() = %q", got)
	}
	if got := (SignedFields{signature: SignatureValue{Label: "sig"}}).SignatureField(); got != "" {
		t.Fatalf("empty-value SignatureField() = %q", got)
	}
	for _, component := range []ComponentIdentifier{
		{Name: "field", Parameters: []Parameter{{Name: "name", Value: "x"}}},
		{Name: "@method", Parameters: []Parameter{{Name: "sf", Value: true}}},
		{Name: "@query-param"},
		{Name: "@status", Parameters: []Parameter{{Name: "req", Value: true}}},
		{Name: "@unknown"},
		{Name: "@method", Parameters: []Parameter{{Name: "unknown", Value: true}}},
	} {
		if validProfileComponent(component) {
			t.Fatalf("validProfileComponent(%#v) = true", component)
		}
	}
	if validEd25519PrivateKey(ed25519.PrivateKey{1}) {
		t.Fatal("short Ed25519 private key accepted")
	}
}

func TestSigningProfileAndKeyValidationRejectEachIndependentBoundary(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	hmacKey, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	provider := signingKeyProviderFunc(func(context.Context) (SigningKey, error) {
		return SigningKey{KeyID: "key", Algorithm: HMACSHA256, Key: hmacKey, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}, nil
	})
	valid := SigningProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256}, CoveredComponents: []ComponentIdentifier{{Name: "@method"}},
		Expires: ParameterRequired, AlgorithmParameter: ParameterRequired, Nonce: ParameterForbidden, Tag: ParameterForbidden,
		Lifetime: time.Second, ResolveTimeout: time.Second, Now: func() time.Time { return now }, Provider: provider,
	}
	if _, err := NewSigningProfile(valid); err != nil {
		t.Fatalf("valid signing profile error = %v", err)
	}
	for _, mutate := range []func(*SigningProfileConfig){
		func(config *SigningProfileConfig) { config.Now = nil },
		func(config *SigningProfileConfig) { config.Provider = nil },
		func(config *SigningProfileConfig) { config.ResolveTimeout = 0 },
		func(config *SigningProfileConfig) { config.ResolveTimeout = -1 },
		func(config *SigningProfileConfig) { config.Lifetime = time.Second - 1 },
		func(config *SigningProfileConfig) { config.Lifetime = time.Second + 1 },
	} {
		config := valid
		mutate(&config)
		if _, err := NewSigningProfile(config); !errors.Is(err, ErrInvalidSigningProfile) {
			t.Fatalf("invalid signing profile error = %v", err)
		}
	}

	validKey := SigningKey{KeyID: "key", Algorithm: HMACSHA256, Key: hmacKey, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}
	for _, mutate := range []func(*SigningKey){
		func(key *SigningKey) { key.KeyID = "" },
		func(key *SigningKey) { key.Key = nil },
		func(key *SigningKey) { key.Revoked = true },
		func(key *SigningKey) { key.NotBefore = time.Time{} },
		func(key *SigningKey) { key.NotAfter = time.Time{} },
		func(key *SigningKey) { key.NotAfter = key.NotBefore },
		func(key *SigningKey) { key.NotBefore = now.Add(time.Nanosecond) },
		func(key *SigningKey) { key.NotAfter = now },
	} {
		key := validKey
		mutate(&key)
		config := valid
		config.Provider = signingKeyProviderFunc(func(context.Context) (SigningKey, error) { return key, nil })
		profile, err := NewSigningProfile(config)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := NewSigner(profile).Sign(context.Background(), MessageContext{Request: newTestRequest(t)}, "sig", SigningOptions{}); !errors.Is(err, ErrSigningKey) {
			t.Fatalf("invalid signing key error = %v", err)
		}
	}
}

func TestSignerRejectsEachProviderKeyAndCryptographicBoundary(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	requestMessage := MessageContext{Request: newTestRequest(t)}
	var nilSigner *Signer
	if _, err := nilSigner.Sign(context.Background(), requestMessage, "sig", SigningOptions{}); !errors.Is(err, ErrSigningPolicy) {
		t.Fatalf("nil signer error = %v", err)
	}
	profile := testSigningProfile(t, now, key)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewSigner(profile).Sign(ctx, requestMessage, "sig", SigningOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled signer error = %v", err)
	}
	//lint:ignore SA1012 This verifies the public nil-context failure contract.
	if _, err := NewSigner(profile).Sign(nil, requestMessage, "sig", SigningOptions{}); !errors.Is(err, context.Canceled) { //nolint:staticcheck // Verifies nil-context rejection.
		t.Fatalf("nil context signer error = %v", err)
	}

	base := func(provider SigningKeyProvider) *SigningProfile {
		config := SigningProfileConfig{
			AllowedAlgorithms: []Algorithm{HMACSHA256}, CoveredComponents: []ComponentIdentifier{{Name: "@method"}},
			Expires: ParameterRequired, AlgorithmParameter: ParameterRequired, Nonce: ParameterForbidden, Tag: ParameterForbidden,
			Lifetime: time.Minute, ResolveTimeout: time.Millisecond, Now: func() time.Time { return now }, Provider: provider,
		}
		profile, err := NewSigningProfile(config)
		if err != nil {
			t.Fatal(err)
		}
		return profile
	}
	providerFailure := base(signingKeyProviderFunc(func(context.Context) (SigningKey, error) { return SigningKey{}, errors.New("private") }))
	if _, err := NewSigner(providerFailure).Sign(context.Background(), requestMessage, "sig", SigningOptions{}); !errors.Is(err, ErrSigningProvider) {
		t.Fatalf("provider error = %v", err)
	}
	timedOut := base(signingKeyProviderFunc(func(ctx context.Context) (SigningKey, error) { <-ctx.Done(); return SigningKey{}, nil }))
	if _, err := NewSigner(timedOut).Sign(context.Background(), requestMessage, "sig", SigningOptions{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("provider timeout error = %v", err)
	}
	wrongAlgorithm := base(signingKeyProviderFunc(func(context.Context) (SigningKey, error) {
		return SigningKey{KeyID: "key", Algorithm: Ed25519, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}, nil
	}))
	if _, err := NewSigner(wrongAlgorithm).Sign(context.Background(), requestMessage, "sig", SigningOptions{}); !errors.Is(err, ErrSigningKey) {
		t.Fatalf("algorithm binding error = %v", err)
	}
	shortValidity := base(signingKeyProviderFunc(func(context.Context) (SigningKey, error) {
		return SigningKey{KeyID: "key", Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(30 * time.Second)}, nil
	}))
	if _, err := NewSigner(shortValidity).Sign(context.Background(), requestMessage, "sig", SigningOptions{}); !errors.Is(err, ErrSigningKey) {
		t.Fatalf("lifetime binding error = %v", err)
	}
	if _, err := NewSigner(profile).Sign(context.Background(), MessageContext{}, "sig", SigningOptions{}); !errors.Is(err, ErrSigningBase) {
		t.Fatalf("signature base error = %v", err)
	}
	edProfile, err := NewSigningProfile(SigningProfileConfig{
		AllowedAlgorithms: []Algorithm{Ed25519}, CoveredComponents: []ComponentIdentifier{{Name: "@method"}},
		Expires: ParameterRequired, AlgorithmParameter: ParameterRequired, Nonce: ParameterForbidden, Tag: ParameterForbidden,
		Lifetime: time.Minute, ResolveTimeout: time.Second, Now: func() time.Time { return now },
		Provider: signingKeyProviderFunc(func(context.Context) (SigningKey, error) {
			return SigningKey{KeyID: "key", Algorithm: Ed25519, Key: ed25519.PrivateKey{1}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewSigner(edProfile).Sign(context.Background(), requestMessage, "sig", SigningOptions{}); !errors.Is(err, ErrSigningKey) {
		t.Fatalf("incompatible signing key error = %v", err)
	}
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rsaProfile, err := NewSigningProfile(SigningProfileConfig{
		AllowedAlgorithms: []Algorithm{RSAPSSSHA512}, CoveredComponents: []ComponentIdentifier{{Name: "@method"}},
		Expires: ParameterRequired, AlgorithmParameter: ParameterRequired, Nonce: ParameterForbidden, Tag: ParameterForbidden,
		Lifetime: time.Minute, ResolveTimeout: time.Second, Now: func() time.Time { return now }, Random: failingReader{},
		Provider: signingKeyProviderFunc(func(context.Context) (SigningKey, error) {
			return SigningKey{KeyID: "key", Algorithm: RSAPSSSHA512, Key: rsaKey, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewSigner(rsaProfile).Sign(context.Background(), requestMessage, "sig", SigningOptions{}); !errors.Is(err, ErrSigningCryptographic) {
		t.Fatalf("signing randomness error = %v", err)
	}
}

func newTestRequest(t *testing.T) *http.Request {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func TestVerificationProfileAndSafeCauseBoundaries(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	base := func() VerificationProfileConfig {
		return VerificationProfileConfig{
			AllowedAlgorithms: []Algorithm{HMACSHA256}, RequiredComponents: []ComponentIdentifier{{Name: "@method"}},
			Created: ParameterRequired, Expires: ParameterRequired, AlgorithmParameter: ParameterRequired,
			Nonce: ParameterForbidden, Tag: ParameterForbidden, MaxAge: time.Minute, ClockSkew: time.Second,
			ResolveTimeout: time.Second, Now: func() time.Time { return now },
			Resolver: resolverFunc(func(context.Context, string) (ResolvedKey, error) {
				return ResolvedKey{Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), FreshUntil: now.Add(time.Minute)}, nil
			}),
		}
	}
	for _, mutate := range []func(*VerificationProfileConfig){
		func(config *VerificationProfileConfig) { config.Created = ParameterForbidden },
		func(config *VerificationProfileConfig) { config.AllowedAlgorithms = []Algorithm{"unsupported"} },
		func(config *VerificationProfileConfig) {
			config.AllowedAlgorithms = []Algorithm{HMACSHA256, HMACSHA256}
		},
		func(config *VerificationProfileConfig) {
			config.RequiredComponents = []ComponentIdentifier{{Name: "@method"}, {Name: "@method"}}
		},
		func(config *VerificationProfileConfig) {
			config.RequiredComponents = []ComponentIdentifier{{Name: "@method", Parameters: []Parameter{{Name: "UPPER", Value: true}}}}
		},
		func(config *VerificationProfileConfig) {
			config.Tag = ParameterRequired
			config.AllowedTags = []string{"tag", "tag"}
		},
	} {
		config := base()
		mutate(&config)
		if _, err := NewVerificationProfile(config); !errors.Is(err, ErrInvalidVerificationProfile) {
			t.Fatalf("invalid verification config error = %v", err)
		}
	}
	for _, test := range []struct {
		input error
		want  error
	}{
		{context.Canceled, context.Canceled}, {context.DeadlineExceeded, context.DeadlineExceeded}, {ErrKeyNotFound, ErrKeyNotFound}, {errors.New("private"), ErrKeyResolutionFailure},
	} {
		if got := safeResolutionCause(test.input); !errors.Is(got, test.want) {
			t.Fatalf("safeResolutionCause(%v) = %v", test.input, got)
		}
	}
	for _, test := range []struct {
		input error
		want  error
	}{
		{context.Canceled, context.Canceled}, {context.DeadlineExceeded, context.DeadlineExceeded}, {ErrReplayDetected, ErrReplayDetected},
		{ErrReplayCapacity, ErrReplayCapacity}, {ErrInvalidReplayRecord, ErrInvalidReplayRecord}, {errors.New("private"), ErrReplayBackendFailure},
	} {
		if got := safeReplayCause(test.input); !errors.Is(got, test.want) {
			t.Fatalf("safeReplayCause(%v) = %v", test.input, got)
		}
	}
	if supportedAlgorithm("unsupported") {
		t.Fatal("unsupported algorithm reported supported")
	}
}

func TestVerificationProfileAndResolvedKeyIndependentBoundaries(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	valid := VerificationProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256}, RequiredComponents: []ComponentIdentifier{{Name: "@method"}},
		Created: ParameterRequired, Expires: ParameterForbidden, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden, MaxAge: time.Second, ClockSkew: time.Second,
		ResolveTimeout: time.Second, Now: func() time.Time { return now },
		Resolver: resolverFunc(func(context.Context, string) (ResolvedKey, error) { return ResolvedKey{}, nil }),
	}
	if _, err := NewVerificationProfile(valid); err != nil {
		t.Fatalf("valid verification profile error = %v", err)
	}
	for _, mutate := range []func(*VerificationProfileConfig){
		func(config *VerificationProfileConfig) { config.Now = nil },
		func(config *VerificationProfileConfig) { config.Resolver = nil },
		func(config *VerificationProfileConfig) { config.ResolveTimeout = 0 },
		func(config *VerificationProfileConfig) { config.ResolveTimeout = -1 },
		func(config *VerificationProfileConfig) { config.ClockSkew = -1 },
		func(config *VerificationProfileConfig) { config.MaxAge = 0 },
		func(config *VerificationProfileConfig) { config.MaxAge = time.Duration(1<<63-1) - config.ClockSkew + 1 },
	} {
		config := valid
		mutate(&config)
		if _, err := NewVerificationProfile(config); !errors.Is(err, ErrInvalidVerificationProfile) {
			t.Fatalf("invalid verification profile error = %v", err)
		}
	}
	for _, component := range []ComponentIdentifier{
		{Name: "field", Parameters: []Parameter{{Name: "name", Value: "x"}}},
		{Name: "@method", Parameters: []Parameter{{Name: "x", Value: []byte("bad")}}},
	} {
		config := valid
		config.RequiredComponents = []ComponentIdentifier{component}
		if _, err := NewVerificationProfile(config); !errors.Is(err, ErrInvalidVerificationProfile) {
			t.Fatalf("invalid required component error = %v", err)
		}
	}
	maximum := valid
	maximum.MaxAge = time.Duration(1<<63-1) - maximum.ClockSkew
	if _, err := NewVerificationProfile(maximum); err != nil {
		t.Fatalf("maximum non-overflowing age error = %v", err)
	}

	profile, _ := NewVerificationProfile(valid)
	validKey := ResolvedKey{
		Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), FreshUntil: now.Add(time.Hour),
	}
	if err := profile.validateKey(validKey, HMACSHA256, true); err != nil {
		t.Fatalf("valid resolved key error = %v", err)
	}
	for _, mutate := range []func(*ResolvedKey){
		func(key *ResolvedKey) { key.Revoked = true },
		func(key *ResolvedKey) { key.Key = nil },
		func(key *ResolvedKey) { key.NotBefore = time.Time{} },
		func(key *ResolvedKey) { key.NotAfter = time.Time{} },
		func(key *ResolvedKey) { key.FreshUntil = time.Time{} },
		func(key *ResolvedKey) { key.NotBefore = now.Add(time.Second + time.Nanosecond) },
		func(key *ResolvedKey) { key.NotAfter = now.Add(-time.Second) },
		func(key *ResolvedKey) { key.FreshUntil = now },
	} {
		resolved := validKey
		mutate(&resolved)
		if err := profile.validateKey(resolved, HMACSHA256, true); !verificationFailureIs(err, VerificationKey) {
			t.Fatalf("invalid resolved key error = %v", err)
		}
	}
	for _, resolved := range []ResolvedKey{
		func() ResolvedKey { value := validKey; value.NotBefore = now.Add(time.Second); return value }(),
		func() ResolvedKey {
			value := validKey
			value.NotAfter = now.Add(-time.Second + time.Nanosecond)
			return value
		}(),
		func() ResolvedKey { value := validKey; value.FreshUntil = now.Add(time.Nanosecond); return value }(),
	} {
		if err := profile.validateKey(resolved, HMACSHA256, true); err != nil {
			t.Fatalf("accepted resolved key boundary error = %v", err)
		}
	}
}

func TestVerifierTimeSelectionAndParameterBoundaries(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	baseConfig := VerificationProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256}, RequiredComponents: []ComponentIdentifier{{Name: "@method"}},
		Created: ParameterRequired, Expires: ParameterForbidden, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden, MaxAge: time.Second, ClockSkew: time.Second,
		ResolveTimeout: time.Second, Now: func() time.Time { return now },
		Resolver: resolverFunc(func(context.Context, string) (ResolvedKey, error) { return ResolvedKey{}, nil }),
	}
	profile, err := NewVerificationProfile(baseConfig)
	if err != nil {
		t.Fatal(err)
	}
	inputAt := func(created time.Time) SignatureInput {
		return SignatureInput{Label: "sig", Components: []ComponentIdentifier{{Name: "@method"}}, Parameters: []Parameter{
			{Name: "created", Value: created.Unix()}, {Name: "keyid", Value: "key"}, {Name: "alg", Value: string(HMACSHA256)},
		}}
	}
	oldest := now.Add(-2 * time.Second)
	metadata, err := profile.validateInput(inputAt(oldest))
	if err != nil {
		t.Fatalf("oldest accepted creation error = %v", err)
	}
	if want := oldest.Add(2 * time.Second); !metadata.replayExpires.Equal(want) {
		t.Fatalf("replay expiration = %v, want %v", metadata.replayExpires, want)
	}
	if _, err := profile.validateInput(inputAt(now.Add(-3 * time.Second))); !verificationFailureIs(err, VerificationTime) {
		t.Fatalf("over-age creation error = %v", err)
	}
	if _, err := profile.validateInput(inputAt(now.Add(time.Second))); err != nil {
		t.Fatalf("future skew boundary error = %v", err)
	}
	if _, err := profile.validateInput(inputAt(now.Add(2 * time.Second))); !verificationFailureIs(err, VerificationTime) {
		t.Fatalf("future creation error = %v", err)
	}

	expiresConfig := baseConfig
	expiresConfig.Created = ParameterForbidden
	expiresConfig.MaxAge = 0
	expiresConfig.Expires = ParameterRequired
	expiresProfile, err := NewVerificationProfile(expiresConfig)
	if err != nil {
		t.Fatal(err)
	}
	expiresInput := SignatureInput{Label: "sig", Components: []ComponentIdentifier{{Name: "@method"}}, Parameters: []Parameter{
		{Name: "expires", Value: now.Unix() + 1}, {Name: "keyid", Value: "key"}, {Name: "alg", Value: string(HMACSHA256)},
	}}
	metadata, err = expiresProfile.validateInput(expiresInput)
	if err != nil || metadata.replayExpires.IsZero() {
		t.Fatalf("expires-only metadata = %#v, error = %v", metadata, err)
	}

	inputs := SignatureInputs{entries: []SignatureInput{{Label: "first"}, {Label: "wanted"}, {Label: "last"}}}
	signatures := Signatures{entries: []SignatureValue{{Label: "first", Value: []byte("1")}, {Label: "wanted", Value: []byte("2")}, {Label: "last", Value: []byte("3")}}}
	selectedInput, selectedSignature, ok := selectSignature("wanted", inputs, signatures)
	if !ok || selectedInput.Label != "wanted" || string(selectedSignature.Value) != "2" {
		t.Fatalf("selected = %#v, %#v, %v", selectedInput, selectedSignature, ok)
	}
	if _, _, ok := selectSignature("missing", inputs, signatures); ok {
		t.Fatal("missing signature selected")
	}
	if _, _, ok := selectSignature("wanted", inputs, Signatures{}); ok {
		t.Fatal("input without signature selected")
	}

	parameterInput := SignatureInput{Parameters: []Parameter{{Name: "integer", Value: int64(1)}, {Name: "text", Value: "x"}, {Name: "wrong", Value: true}}}
	if value, ok := integerParameter(parameterInput, "integer"); !ok || value != 1 {
		t.Fatalf("integer parameter = %d, %v", value, ok)
	}
	if _, ok := integerParameter(parameterInput, "missing"); ok {
		t.Fatal("missing integer parameter present")
	}
	if _, ok := integerParameter(parameterInput, "wrong"); ok {
		t.Fatal("wrong-type integer parameter present")
	}
	if value, ok := stringParameter(parameterInput, "text"); !ok || value != "x" {
		t.Fatalf("string parameter = %q, %v", value, ok)
	}
	if _, ok := stringParameter(parameterInput, "missing"); ok {
		t.Fatal("missing string parameter present")
	}
	if _, ok := stringParameter(parameterInput, "wrong"); ok {
		t.Fatal("wrong-type string parameter present")
	}

	if _, err := NewVerifier(nil).Verify(context.Background(), MessageContext{}, "sig", SignatureInputs{}, Signatures{}); !verificationFailureIs(err, VerificationSelection) {
		t.Fatalf("nil-profile verifier error = %v", err)
	}
	if _, err := NewVerifier(nil).Verify(context.Background(), MessageContext{}, "sig", SignatureInputs{}, Signatures{}); !errors.Is(err, ErrInvalidVerificationProfile) {
		t.Fatalf("nil-profile verifier cause = %v", err)
	}
	if _, err := NewVerifier(profile).Verify(context.Background(), MessageContext{}, "", SignatureInputs{}, Signatures{}); !errors.Is(err, ErrInvalidVerificationProfile) {
		t.Fatalf("empty-label verifier error = %v", err)
	}

	orderedConfig := baseConfig
	orderedConfig.Expires = ParameterRequired
	orderedProfile, err := NewVerificationProfile(orderedConfig)
	if err != nil {
		t.Fatal(err)
	}
	orderedInput := inputAt(now)
	orderedInput.Parameters = append(orderedInput.Parameters, Parameter{Name: "expires", Value: now.Unix()})
	if _, err := orderedProfile.validateInput(orderedInput); !verificationFailureIs(err, VerificationTime) {
		t.Fatalf("non-increasing expiration error = %v", err)
	}
}

func TestVerifierSelectionInputAndKeyBoundaries(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	profile := testHTTPVerificationProfile(t, now, key)
	verifier := NewVerifier(profile)
	message := MessageContext{Request: newTestRequest(t)}
	signed, err := NewSigner(testSigningProfile(t, now, key)).Sign(context.Background(), message, "sig", SigningOptions{})
	if err != nil {
		t.Fatal(err)
	}
	signedInputs, err := ParseSignatureInputs([]string{signed.SignatureInputField()})
	if err != nil {
		t.Fatal(err)
	}
	signedSignatures, err := ParseSignatures([]string{signed.SignatureField()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := verifier.Verify(ctx, message, "sig", SignatureInputs{}, Signatures{}); err == nil {
		t.Fatal("cancelled verification succeeded")
	}
	//lint:ignore SA1012 This verifies the public nil-context failure contract.
	if _, err := verifier.Verify(nil, message, "sig", SignatureInputs{}, Signatures{}); err == nil { //nolint:staticcheck // Verifies nil-context rejection.
		t.Fatal("nil context verification succeeded")
	}
	var nilVerifier *Verifier
	if _, err := nilVerifier.Verify(context.Background(), message, "sig", SignatureInputs{}, Signatures{}); err == nil {
		t.Fatal("nil verifier succeeded")
	}
	inputs := SignatureInputs{entries: []SignatureInput{{Label: "sig"}}}
	if _, err := verifier.Verify(context.Background(), message, "sig", inputs, Signatures{}); err == nil {
		t.Fatal("mismatched label sets succeeded")
	}
	if _, err := verifier.Verify(context.Background(), message, "other", inputs, Signatures{entries: []SignatureValue{{Label: "sig", Value: []byte("x")}}}); err == nil {
		t.Fatal("missing selected label succeeded")
	}
	unusableProfile := *profile
	unusableProfile.resolver = resolverFunc(func(context.Context, string) (ResolvedKey, error) { return ResolvedKey{}, nil })
	if _, err := NewVerifier(&unusableProfile).Verify(context.Background(), message, "sig", signedInputs, signedSignatures); err == nil {
		t.Fatal("unusable resolved key succeeded")
	}
	if _, err := verifier.Verify(context.Background(), MessageContext{}, "sig", signedInputs, signedSignatures); err == nil {
		t.Fatal("invalid signature base succeeded")
	}
	incompatibleProfile := *profile
	incompatibleProfile.resolver = resolverFunc(func(context.Context, string) (ResolvedKey, error) {
		return ResolvedKey{Algorithm: HMACSHA256, Key: "wrong", NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), FreshUntil: now.Add(time.Minute)}, nil
	})
	if _, err := NewVerifier(&incompatibleProfile).Verify(context.Background(), message, "sig", signedInputs, signedSignatures); err == nil {
		t.Fatal("incompatible verification key succeeded")
	} else {
		var typed *VerificationError
		if !errors.As(err, &typed) || typed.Failure != VerificationKey {
			t.Fatalf("incompatible verification error = %#v", err)
		}
	}

	minimal := &VerificationProfile{
		algorithms: map[Algorithm]struct{}{HMACSHA256: {}}, requiredComponents: []ComponentIdentifier{{Name: "@method"}},
		created: ParameterRequired, expires: ParameterRequired, algorithmParameter: ParameterRequired,
		nonce: ParameterForbidden, tag: ParameterForbidden, maxAge: time.Minute, clockSkew: time.Second, now: func() time.Time { return now },
	}
	for _, input := range []SignatureInput{
		{Label: "sig"},
		{Label: "sig", Components: []ComponentIdentifier{{Name: "@path"}}},
		{Label: "sig", Components: []ComponentIdentifier{{Name: "@method"}}, Parameters: []Parameter{{Name: "created", Value: now.Add(time.Minute).Unix()}}},
		{Label: "sig", Components: []ComponentIdentifier{{Name: "@method"}}, Parameters: []Parameter{{Name: "created", Value: now.Unix()}, {Name: "expires", Value: now.Unix()}}},
		{Label: "sig", Components: []ComponentIdentifier{{Name: "@method"}}, Parameters: []Parameter{{Name: "created", Value: now.Unix()}, {Name: "expires", Value: now.Add(time.Minute).Unix()}, {Name: "alg", Value: "ed25519"}}},
	} {
		if _, err := minimal.validateInput(input); err == nil {
			t.Fatalf("validateInput(%#v) succeeded", input.Parameters)
		}
	}
	baseline := SignatureInput{Label: "sig", Components: []ComponentIdentifier{{Name: "@method"}}, Parameters: []Parameter{
		{Name: "created", Value: now.Unix()}, {Name: "expires", Value: now.Add(time.Minute).Unix()},
		{Name: "alg", Value: string(HMACSHA256)}, {Name: "keyid", Value: "key"},
	}}
	for _, test := range []struct {
		name    string
		profile func() *VerificationProfile
		input   SignatureInput
	}{
		{name: "invalid component", profile: func() *VerificationProfile { return minimal }, input: SignatureInput{Label: "sig", Components: []ComponentIdentifier{{Name: "@method", Parameters: []Parameter{{Name: "UPPER", Value: true}}}}}},
		{name: "created missing", profile: func() *VerificationProfile { return minimal }, input: SignatureInput{Label: "sig", Components: baseline.Components, Parameters: baseline.Parameters[1:]}},
		{name: "expires missing", profile: func() *VerificationProfile { return minimal }, input: SignatureInput{Label: "sig", Components: baseline.Components, Parameters: append([]Parameter{baseline.Parameters[0]}, baseline.Parameters[2:]...)}},
		{name: "algorithm missing", profile: func() *VerificationProfile { return minimal }, input: SignatureInput{Label: "sig", Components: baseline.Components, Parameters: append(append([]Parameter{}, baseline.Parameters[:2]...), baseline.Parameters[3])}},
		{name: "key missing", profile: func() *VerificationProfile { return minimal }, input: SignatureInput{Label: "sig", Components: baseline.Components, Parameters: baseline.Parameters[:3]}},
		{name: "nonce empty", profile: func() *VerificationProfile { copy := *minimal; copy.nonce = ParameterRequired; return &copy }, input: SignatureInput{Label: "sig", Components: baseline.Components, Parameters: append(append([]Parameter{}, baseline.Parameters...), Parameter{Name: "nonce", Value: ""})}},
		{name: "nonce unbounded", profile: func() *VerificationProfile {
			copy := *minimal
			copy.created = ParameterForbidden
			copy.expires = ParameterForbidden
			copy.nonce = ParameterRequired
			return &copy
		}, input: SignatureInput{Label: "sig", Components: baseline.Components, Parameters: []Parameter{{Name: "alg", Value: string(HMACSHA256)}, {Name: "keyid", Value: "key"}, {Name: "nonce", Value: "nonce"}}}},
		{name: "tag missing", profile: func() *VerificationProfile {
			copy := *minimal
			copy.tag = ParameterRequired
			copy.allowedTags = map[string]struct{}{"allowed": {}}
			return &copy
		}, input: baseline},
		{name: "tag denied", profile: func() *VerificationProfile {
			copy := *minimal
			copy.tag = ParameterRequired
			copy.allowedTags = map[string]struct{}{"allowed": {}}
			return &copy
		}, input: SignatureInput{Label: "sig", Components: baseline.Components, Parameters: append(append([]Parameter{}, baseline.Parameters...), Parameter{Name: "tag", Value: "denied"})}},
	} {
		if _, err := test.profile().validateInput(test.input); err == nil {
			t.Fatalf("validateInput(%s) succeeded", test.name)
		}
	}
	validKey := ResolvedKey{Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), FreshUntil: now.Add(time.Minute)}
	if err := minimal.validateKey(ResolvedKey{Algorithm: Ed25519, Key: key, NotBefore: validKey.NotBefore, NotAfter: validKey.NotAfter, FreshUntil: validKey.FreshUntil}, "", false); err == nil {
		t.Fatal("disallowed resolved algorithm succeeded")
	}
	if err := minimal.validateKey(validKey, Ed25519, true); err == nil {
		t.Fatal("mismatched algorithm parameter succeeded")
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
