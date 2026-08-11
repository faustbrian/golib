package consumer

import (
	"context"
	"net/http"
	"testing"
	"time"

	httpsignature "github.com/faustbrian/golib/pkg/http-signature"
	"github.com/faustbrian/golib/pkg/http-signature/compatibility"
)

type provider struct{ key httpsignature.SigningKey }

func (value provider) SigningKey(context.Context) (httpsignature.SigningKey, error) {
	return value.key, nil
}

type resolver struct{ key httpsignature.ResolvedKey }

func (value resolver) Resolve(context.Context, string) (httpsignature.ResolvedKey, error) {
	return value.key, nil
}

func TestPublicAPIFromCleanModule(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	key, err := httpsignature.NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	components := []httpsignature.ComponentIdentifier{{Name: "@method"}, {Name: "@authority"}}
	signingProfile, err := httpsignature.NewSigningProfile(httpsignature.SigningProfileConfig{
		AllowedAlgorithms: componentsAlgorithms(), CoveredComponents: components,
		Expires: httpsignature.ParameterForbidden, AlgorithmParameter: httpsignature.ParameterRequired,
		Nonce: httpsignature.ParameterForbidden, Tag: httpsignature.ParameterForbidden,
		ResolveTimeout: time.Second, Now: func() time.Time { return now },
		Provider: provider{key: httpsignature.SigningKey{
			KeyID: "clean-consumer", Algorithm: httpsignature.HMACSHA256, Key: key,
			NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Minute),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodGet, "https://example.test/", nil)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := httpsignature.NewSigner(signingProfile).Sign(context.Background(), httpsignature.MessageContext{
		Request: request, MaxSignatureBaseBytes: 4096,
	}, "sig", httpsignature.SigningOptions{})
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := httpsignature.ParseSignatureInputs([]string{signed.SignatureInputField()})
	if err != nil {
		t.Fatal(err)
	}
	signatures, err := httpsignature.ParseSignatures([]string{signed.SignatureField()})
	if err != nil {
		t.Fatal(err)
	}
	verificationProfile, err := httpsignature.NewVerificationProfile(httpsignature.VerificationProfileConfig{
		AllowedAlgorithms: componentsAlgorithms(), RequiredComponents: components,
		Created: httpsignature.ParameterRequired, Expires: httpsignature.ParameterForbidden,
		AlgorithmParameter: httpsignature.ParameterRequired, Nonce: httpsignature.ParameterForbidden,
		Tag: httpsignature.ParameterForbidden, MaxAge: time.Minute, ClockSkew: time.Second,
		ResolveTimeout: time.Second, Now: func() time.Time { return now },
		Resolver: resolver{key: httpsignature.ResolvedKey{
			Algorithm: httpsignature.HMACSHA256, Key: key, NotBefore: now.Add(-time.Minute),
			NotAfter: now.Add(time.Minute), FreshUntil: now.Add(time.Minute),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := httpsignature.NewVerifier(verificationProfile).Verify(context.Background(), httpsignature.MessageContext{
		Request: request, MaxSignatureBaseBytes: 4096,
	}, "sig", inputs, signatures); err != nil {
		t.Fatal(err)
	}
	if compatibility.CavageDraft == "" {
		t.Fatal("compatibility protocol constant is unavailable")
	}
}

func componentsAlgorithms() []httpsignature.Algorithm {
	return []httpsignature.Algorithm{httpsignature.HMACSHA256}
}
