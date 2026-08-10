package httpsignature_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	httpsignature "github.com/faustbrian/golib/pkg/http-signature"
)

type exampleProvider struct {
	key httpsignature.SigningKey
}

func (provider exampleProvider) SigningKey(context.Context) (httpsignature.SigningKey, error) {
	return provider.key, nil
}

type exampleResolver struct {
	key httpsignature.ResolvedKey
}

func (resolver exampleResolver) Resolve(context.Context, string) (httpsignature.ResolvedKey, error) {
	return resolver.key, nil
}

func Example_requestSignAndVerify() {
	now := time.Unix(1_700_000_000, 0)
	key, _ := httpsignature.NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	signingProfile, _ := httpsignature.NewSigningProfile(httpsignature.SigningProfileConfig{
		AllowedAlgorithms:  []httpsignature.Algorithm{httpsignature.HMACSHA256},
		CoveredComponents:  []httpsignature.ComponentIdentifier{{Name: "@method"}, {Name: "@authority"}},
		Expires:            httpsignature.ParameterForbidden,
		AlgorithmParameter: httpsignature.ParameterRequired,
		Nonce:              httpsignature.ParameterForbidden,
		Tag:                httpsignature.ParameterForbidden,
		ResolveTimeout:     time.Second,
		Now:                func() time.Time { return now },
		Provider: exampleProvider{key: httpsignature.SigningKey{
			KeyID: "key-2026", Algorithm: httpsignature.HMACSHA256, Key: key,
			NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		}},
	})
	request, _ := http.NewRequest(http.MethodPost, "https://api.example/pay", nil)
	signed, _ := httpsignature.NewSigner(signingProfile).Sign(
		context.Background(), httpsignature.MessageContext{Request: request}, "sig", httpsignature.SigningOptions{},
	)

	inputs, _ := httpsignature.ParseSignatureInputs([]string{signed.SignatureInputField()})
	signatures, _ := httpsignature.ParseSignatures([]string{signed.SignatureField()})
	verificationProfile, _ := httpsignature.NewVerificationProfile(httpsignature.VerificationProfileConfig{
		AllowedAlgorithms:  []httpsignature.Algorithm{httpsignature.HMACSHA256},
		RequiredComponents: []httpsignature.ComponentIdentifier{{Name: "@method"}, {Name: "@authority"}},
		Created:            httpsignature.ParameterRequired,
		Expires:            httpsignature.ParameterForbidden,
		AlgorithmParameter: httpsignature.ParameterRequired,
		Nonce:              httpsignature.ParameterForbidden,
		Tag:                httpsignature.ParameterForbidden,
		MaxAge:             time.Minute,
		ClockSkew:          time.Second,
		ResolveTimeout:     time.Second,
		Now:                func() time.Time { return now },
		Resolver: exampleResolver{key: httpsignature.ResolvedKey{
			Algorithm: httpsignature.HMACSHA256, Key: key,
			NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), FreshUntil: now.Add(time.Minute),
		}},
	})
	verified, err := httpsignature.NewVerifier(verificationProfile).Verify(
		context.Background(), httpsignature.MessageContext{Request: request}, "sig", inputs, signatures,
	)
	fmt.Println(err == nil, verified.Label, verified.Algorithm)
	// Output: true sig hmac-sha256
}

func Example_clientAndServerMiddleware() {
	now := time.Unix(1_700_000_000, 0)
	key, _ := httpsignature.NewHMACKey([]byte("0123456789abcdef0123456789abcdef"))
	components := []httpsignature.ComponentIdentifier{
		{Name: "@method"}, {Name: "@authority"}, {Name: "content-digest"},
	}
	signingProfile, _ := httpsignature.NewSigningProfile(httpsignature.SigningProfileConfig{
		AllowedAlgorithms: []httpsignature.Algorithm{httpsignature.HMACSHA256}, CoveredComponents: components,
		Expires: httpsignature.ParameterForbidden, AlgorithmParameter: httpsignature.ParameterRequired,
		Nonce: httpsignature.ParameterForbidden, Tag: httpsignature.ParameterForbidden,
		ResolveTimeout: time.Second, Now: func() time.Time { return now },
		Provider: exampleProvider{key: httpsignature.SigningKey{
			KeyID: "key-2026", Algorithm: httpsignature.HMACSHA256, Key: key,
			NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		}},
	})
	verificationProfile, _ := httpsignature.NewVerificationProfile(httpsignature.VerificationProfileConfig{
		AllowedAlgorithms: []httpsignature.Algorithm{httpsignature.HMACSHA256}, RequiredComponents: components,
		Created: httpsignature.ParameterRequired, Expires: httpsignature.ParameterForbidden,
		AlgorithmParameter: httpsignature.ParameterRequired, Nonce: httpsignature.ParameterForbidden,
		Tag: httpsignature.ParameterForbidden, MaxAge: time.Minute, ClockSkew: time.Second,
		ResolveTimeout: time.Second, Now: func() time.Time { return now },
		Resolver: exampleResolver{key: httpsignature.ResolvedKey{
			Algorithm: httpsignature.HMACSHA256, Key: key,
			NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), FreshUntil: now.Add(time.Minute),
		}},
	})

	mapError := func(writer http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(writer, "invalid signed request", http.StatusUnauthorized)
	}
	verifySignature, _ := httpsignature.NewRequestVerificationMiddleware(
		httpsignature.RequestVerificationMiddlewareConfig{
			Verifier: httpsignature.NewVerifier(verificationProfile),
			SelectLabel: func(*http.Request, httpsignature.SignatureInputs, httpsignature.Signatures) (string, error) {
				return "sig", nil
			},
			MapError: mapError,
		},
	)
	verifyDigest, _ := httpsignature.NewBufferedContentDigestVerificationMiddleware(
		httpsignature.BufferedContentDigestVerificationMiddlewareConfig{
			RequiredAlgorithms: []httpsignature.DigestAlgorithm{httpsignature.SHA256},
			MaxBytes:           1024, MapError: mapError,
		},
	)
	server := httptest.NewServer(verifyDigest(verifySignature(http.HandlerFunc(
		func(writer http.ResponseWriter, request *http.Request) {
			body, _ := io.ReadAll(request.Body)
			_, verified := httpsignature.VerifiedSignatureFromContext(request.Context())
			_, _ = fmt.Fprintln(writer, verified, string(body))
		},
	))))
	defer server.Close()

	signingTransport, _ := httpsignature.NewSigningRoundTripper(httpsignature.SigningRoundTripperConfig{
		Transport: server.Client().Transport, Signer: httpsignature.NewSigner(signingProfile), Label: "sig",
		Existing: httpsignature.ExistingSignaturesReject,
		Options: func(context.Context, *http.Request) (httpsignature.SigningOptions, error) {
			return httpsignature.SigningOptions{}, nil
		},
	})
	digestTransport, _ := httpsignature.NewBufferedContentDigestRoundTripper(
		httpsignature.BufferedContentDigestRoundTripperConfig{
			Transport: signingTransport, Algorithms: []httpsignature.DigestAlgorithm{httpsignature.SHA256}, MaxBytes: 1024,
		},
	)
	client := &http.Client{Transport: digestTransport}
	response, _ := client.Post(server.URL+"/messages", "text/plain", strings.NewReader("signed payload"))
	responseBody, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	fmt.Print(response.StatusCode, " ", string(responseBody))
	// Output: 200 true signed payload
}
