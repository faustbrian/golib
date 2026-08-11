package httpsignature

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"net/http"
	"testing"
	"time"
)

type signingKeyProviderFunc func(context.Context) (SigningKey, error)

func (provider signingKeyProviderFunc) SigningKey(ctx context.Context) (SigningKey, error) {
	return provider(ctx)
}

type deterministicRandomReader struct {
	reads int
}

func (reader *deterministicRandomReader) Read(buffer []byte) (int, error) {
	reader.reads++
	clear(buffer)
	return len(buffer), nil
}

func TestSignerCreatesDeterministicFieldsAcceptedByVerifier(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, err := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewHMACKey() error = %v", err)
	}
	signingProfile, err := NewSigningProfile(SigningProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256},
		CoveredComponents: []ComponentIdentifier{
			{Name: "@method"},
			{Name: "@authority"},
		},
		Expires:            ParameterRequired,
		AlgorithmParameter: ParameterRequired,
		Nonce:              ParameterRequired,
		Tag:                ParameterRequired,
		TagValue:           "payment",
		Lifetime:           time.Minute,
		ResolveTimeout:     time.Second,
		Now:                func() time.Time { return now },
		Provider: signingKeyProviderFunc(func(context.Context) (SigningKey, error) {
			return SigningKey{
				KeyID:     "key-1",
				Algorithm: HMACSHA256,
				Key:       key,
				NotBefore: now.Add(-time.Hour),
				NotAfter:  now.Add(time.Hour),
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewSigningProfile() error = %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, "https://example.com/pay", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	signed, err := NewSigner(signingProfile).Sign(
		context.Background(),
		MessageContext{Request: request},
		"sig",
		SigningOptions{Nonce: "nonce-1"},
	)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	const wantInput = `sig=("@method" "@authority");created=1700000000;expires=1700000060;nonce="nonce-1";keyid="key-1";alg="hmac-sha256";tag="payment"`
	if got := signed.SignatureInputField(); got != wantInput {
		t.Fatalf("SignatureInputField() = %q, want %q", got, wantInput)
	}
	if got := signed.SignatureField(); got == "" {
		t.Fatal("SignatureField() is empty")
	}
	second, err := NewSigner(signingProfile).Sign(
		context.Background(),
		MessageContext{Request: request},
		"sig2",
		SigningOptions{Nonce: "nonce-2"},
	)
	if err != nil {
		t.Fatalf("Sign(second) error = %v", err)
	}
	combinedInputs, combinedSignatures, err := CombineSignedFields(second, signed)
	if err != nil {
		t.Fatalf("CombineSignedFields() error = %v", err)
	}
	const wantCombinedInputs = `sig2=("@method" "@authority");created=1700000000;expires=1700000060;nonce="nonce-2";keyid="key-1";alg="hmac-sha256";tag="payment", ` + wantInput
	if got := combinedInputs.String(); got != wantCombinedInputs {
		t.Fatalf("combined Signature-Input = %q, want %q", got, wantCombinedInputs)
	}
	if got := combinedSignatures.String(); got[:5] != "sig2=" {
		t.Fatalf("combined Signature = %q, want caller order", got)
	}
	if _, _, err := CombineSignedFields(signed, signed); !errors.Is(err, ErrInvalidSignedFields) {
		t.Fatalf("CombineSignedFields(duplicate) error = %v, want ErrInvalidSignedFields", err)
	}

	inputs, err := ParseSignatureInputs([]string{signed.SignatureInputField()})
	if err != nil {
		t.Fatalf("ParseSignatureInputs() error = %v", err)
	}
	signatures, err := ParseSignatures([]string{signed.SignatureField()})
	if err != nil {
		t.Fatalf("ParseSignatures() error = %v", err)
	}
	replay, _ := NewMemoryReplayStore(MemoryReplayConfig{Capacity: 1, MaxTTL: 2 * time.Minute, MaxKeyIDBytes: 64, MaxNonceBytes: 64, Now: func() time.Time { return now }})
	verificationProfile, err := NewVerificationProfile(VerificationProfileConfig{
		AllowedAlgorithms:  []Algorithm{HMACSHA256},
		RequiredComponents: []ComponentIdentifier{{Name: "@method"}, {Name: "@authority"}},
		Created:            ParameterRequired,
		Expires:            ParameterRequired,
		AlgorithmParameter: ParameterRequired,
		Nonce:              ParameterRequired,
		Tag:                ParameterRequired,
		AllowedTags:        []string{"payment"},
		MaxAge:             time.Minute,
		ClockSkew:          time.Second,
		ResolveTimeout:     time.Second,
		ReplayTimeout:      time.Second,
		Now:                func() time.Time { return now },
		Resolver: resolverFunc(func(context.Context, string) (ResolvedKey, error) {
			return ResolvedKey{Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), FreshUntil: now.Add(time.Minute)}, nil
		}),
		Replay: replay,
	})
	if err != nil {
		t.Fatalf("NewVerificationProfile() error = %v", err)
	}
	if _, err := NewVerifier(verificationProfile).Verify(context.Background(), MessageContext{Request: request}, "sig", inputs, signatures); err != nil {
		t.Fatalf("Verify() signed fields error = %v", err)
	}
}

func TestSigningProfileRequiresExplicitCoherentPolicy(t *testing.T) {
	t.Parallel()

	valid := SigningProfileConfig{
		AllowedAlgorithms:  []Algorithm{HMACSHA256},
		CoveredComponents:  []ComponentIdentifier{{Name: "@method"}},
		Expires:            ParameterForbidden,
		AlgorithmParameter: ParameterRequired,
		Nonce:              ParameterForbidden,
		Tag:                ParameterForbidden,
		ResolveTimeout:     time.Second,
		Now:                time.Now,
		Provider:           signingKeyProviderFunc(func(context.Context) (SigningKey, error) { return SigningKey{}, nil }),
	}

	for _, config := range []SigningProfileConfig{
		{},
		func() SigningProfileConfig { value := valid; value.AllowedAlgorithms = nil; return value }(),
		func() SigningProfileConfig { value := valid; value.CoveredComponents = nil; return value }(),
		func() SigningProfileConfig { value := valid; value.Expires = ParameterOptional; return value }(),
		func() SigningProfileConfig { value := valid; value.Nonce = ParameterOptional; return value }(),
		func() SigningProfileConfig { value := valid; value.Tag = ParameterRequired; return value }(),
		func() SigningProfileConfig {
			value := valid
			value.Tag = ParameterRequired
			value.TagValue = "bad\nvalue"
			return value
		}(),
		func() SigningProfileConfig { value := valid; value.Expires = ParameterRequired; return value }(),
		func() SigningProfileConfig {
			value := valid
			value.Expires = ParameterRequired
			value.Lifetime = time.Nanosecond
			return value
		}(),
		func() SigningProfileConfig {
			value := valid
			value.CoveredComponents = []ComponentIdentifier{{Name: "@method", Parameters: []Parameter{{Name: "sf", Value: true}}}}
			return value
		}(),
	} {
		if _, err := NewSigningProfile(config); !errors.Is(err, ErrInvalidSigningProfile) {
			t.Fatalf("NewSigningProfile() error = %v, want ErrInvalidSigningProfile", err)
		}
	}

	for _, algorithm := range []Algorithm{RSAPSSSHA512, ECDSAP256SHA256, ECDSAP384SHA384} {
		randomizedWithoutCallerRandom := valid
		randomizedWithoutCallerRandom.AllowedAlgorithms = []Algorithm{algorithm}
		if _, err := NewSigningProfile(randomizedWithoutCallerRandom); err != nil {
			t.Fatalf("NewSigningProfile(%s without caller random) error = %v", algorithm, err)
		}
	}
}

func TestSigningErrorMappingPreservesPublicFailureCategories(t *testing.T) {
	t.Parallel()

	if err := mapSigningError(nil); err != nil {
		t.Fatalf("mapSigningError(nil) = %v", err)
	}
	if err := mapSigningError(ErrIncompatibleKey); !errors.Is(err, ErrSigningKey) {
		t.Fatalf("mapSigningError(incompatible key) = %v, want ErrSigningKey", err)
	}
	if err := mapSigningError(ErrSignatureRandomness); !errors.Is(err, ErrSigningCryptographic) {
		t.Fatalf("mapSigningError(randomness failure) = %v, want ErrSigningCryptographic", err)
	}
}

func TestSignerIgnoresDeprecatedCallerRandomness(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	configuredRandom := &deterministicRandomReader{}
	profile, err := NewSigningProfile(SigningProfileConfig{
		AllowedAlgorithms:  []Algorithm{RSAPSSSHA512},
		CoveredComponents:  []ComponentIdentifier{{Name: "@method"}},
		Expires:            ParameterForbidden,
		AlgorithmParameter: ParameterRequired,
		Nonce:              ParameterForbidden,
		Tag:                ParameterForbidden,
		ResolveTimeout:     time.Second,
		Now:                func() time.Time { return now },
		Provider: signingKeyProviderFunc(func(context.Context) (SigningKey, error) {
			return SigningKey{KeyID: "key", Algorithm: RSAPSSSHA512, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}, nil
		}),
		Random: configuredRandom,
	})
	if err != nil {
		t.Fatalf("NewSigningProfile() error = %v", err)
	}
	request, err := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	message := MessageContext{Request: request}
	signed, err := NewSigner(profile).Sign(context.Background(), message, "sig", SigningOptions{})
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	if configuredRandom.reads != 0 {
		t.Fatalf("configured random reads = %d, want 0", configuredRandom.reads)
	}
	base, err := CreateSignatureBase(message, signed.input)
	if err != nil {
		t.Fatalf("CreateSignatureBase() error = %v", err)
	}
	if err := Verify(context.Background(), RSAPSSSHA512, &key.PublicKey, []byte(base), signed.signature.Value); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestSignerRejectsMissingNonceAndExpiredOrMismatchedKey(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	request, _ := http.NewRequest(http.MethodGet, "https://example.com/", nil)

	for _, signingKey := range []SigningKey{
		{KeyID: "key", Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(-time.Second)},
		{KeyID: "key", Algorithm: Ed25519, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)},
	} {
		profile, err := NewSigningProfile(SigningProfileConfig{
			AllowedAlgorithms:  []Algorithm{HMACSHA256},
			CoveredComponents:  []ComponentIdentifier{{Name: "@method"}},
			Expires:            ParameterForbidden,
			AlgorithmParameter: ParameterRequired,
			Nonce:              ParameterRequired,
			Tag:                ParameterForbidden,
			ResolveTimeout:     time.Second,
			Now:                func() time.Time { return now },
			Provider:           signingKeyProviderFunc(func(context.Context) (SigningKey, error) { return signingKey, nil }),
		})
		if err != nil {
			t.Fatalf("NewSigningProfile() error = %v", err)
		}
		if _, err := NewSigner(profile).Sign(context.Background(), MessageContext{Request: request}, "sig", SigningOptions{}); !errors.Is(err, ErrSigningPolicy) {
			t.Fatalf("Sign() missing nonce error = %v, want ErrSigningPolicy", err)
		}
		if _, err := NewSigner(profile).Sign(context.Background(), MessageContext{Request: request}, "sig", SigningOptions{Nonce: "nonce"}); !errors.Is(err, ErrSigningKey) {
			t.Fatalf("Sign() key error = %v, want ErrSigningKey", err)
		}
	}
}

func TestSignerRejectsInvalidSignatureLabelsBeforeKeyResolution(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	profile, err := NewSigningProfile(SigningProfileConfig{
		AllowedAlgorithms:  []Algorithm{HMACSHA256},
		CoveredComponents:  []ComponentIdentifier{{Name: "@method"}},
		Expires:            ParameterForbidden,
		AlgorithmParameter: ParameterRequired,
		Nonce:              ParameterForbidden,
		Tag:                ParameterForbidden,
		ResolveTimeout:     time.Second,
		Now:                func() time.Time { return now },
		Provider: signingKeyProviderFunc(func(context.Context) (SigningKey, error) {
			return SigningKey{KeyID: "key", Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Minute)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewSigningProfile() error = %v", err)
	}
	request, _ := http.NewRequest(http.MethodGet, "https://example.com/", nil)

	for _, label := range []string{"", "Upper", "sig,other", "sig space"} {
		if _, err := NewSigner(profile).Sign(context.Background(), MessageContext{Request: request}, label, SigningOptions{}); !errors.Is(err, ErrSigningPolicy) {
			t.Fatalf("Sign(%q) error = %v, want ErrSigningPolicy", label, err)
		}
	}
}

func TestZeroSignedFieldsSerializeAsEmpty(t *testing.T) {
	t.Parallel()

	var signed SignedFields
	if got := signed.SignatureInputField(); got != "" {
		t.Fatalf("SignatureInputField() = %q, want empty", got)
	}
	if got := signed.SignatureField(); got != "" {
		t.Fatalf("SignatureField() = %q, want empty", got)
	}
}

func TestProfilesRequireTrustedExternalContextBeforeKeyAccess(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, _ := NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	providerCalls := 0
	signingProfile, err := NewSigningProfile(SigningProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256}, CoveredComponents: []ComponentIdentifier{{Name: "@authority"}},
		Expires: ParameterRequired, AlgorithmParameter: ParameterRequired, Nonce: ParameterForbidden, Tag: ParameterForbidden,
		Lifetime: time.Minute, ResolveTimeout: time.Second, RequireExternalRequestContext: true,
		Now: func() time.Time { return now }, Provider: signingKeyProviderFunc(func(context.Context) (SigningKey, error) {
			providerCalls++
			return SigningKey{KeyID: "key", Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewSigningProfile() error = %v", err)
	}
	request, _ := http.NewRequest(http.MethodGet, "http://internal.example/path", nil)
	signer := NewSigner(signingProfile)
	if _, err := signer.Sign(context.Background(), MessageContext{Request: request}, "sig", SigningOptions{}); !errors.Is(err, ErrSigningPolicy) || providerCalls != 0 {
		t.Fatalf("Sign(missing external context) error = %v, provider calls = %d", err, providerCalls)
	}
	external := &ExternalRequestContext{Scheme: "https", Authority: "example.com", RequestTarget: "/path"}
	signed, err := signer.Sign(context.Background(), MessageContext{Request: request, ExternalRequest: external}, "sig", SigningOptions{})
	if err != nil {
		t.Fatalf("Sign(external context) error = %v", err)
	}

	resolverCalls := 0
	verificationProfile, err := NewVerificationProfile(VerificationProfileConfig{
		AllowedAlgorithms: []Algorithm{HMACSHA256}, RequiredComponents: []ComponentIdentifier{{Name: "@authority"}},
		Created: ParameterRequired, Expires: ParameterRequired, AlgorithmParameter: ParameterRequired,
		Nonce: ParameterForbidden, Tag: ParameterForbidden, MaxAge: time.Minute, ClockSkew: time.Second,
		ResolveTimeout: time.Second, RequireExternalRequestContext: true, Now: func() time.Time { return now },
		Resolver: resolverFunc(func(context.Context, string) (ResolvedKey, error) {
			resolverCalls++
			return ResolvedKey{Algorithm: HMACSHA256, Key: key, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), FreshUntil: now.Add(time.Minute)}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewVerificationProfile() error = %v", err)
	}
	inputs, _ := ParseSignatureInputs([]string{signed.SignatureInputField()})
	signatures, _ := ParseSignatures([]string{signed.SignatureField()})
	if _, err := NewVerifier(verificationProfile).Verify(context.Background(), MessageContext{Request: request}, "sig", inputs, signatures); !verificationFailureIs(err, VerificationPolicy) || resolverCalls != 0 {
		t.Fatalf("Verify(missing external context) error = %v, resolver calls = %d", err, resolverCalls)
	}
}
