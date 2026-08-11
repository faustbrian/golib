package comparison_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	dadrus "github.com/dadrus/httpsig"
	httpsignature "github.com/faustbrian/golib/pkg/http-signature"
	peer "github.com/yaronf/httpsign"
)

var benchmarkKey = []byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")

type signingProvider struct{ key httpsignature.SigningKey }

func (provider signingProvider) SigningKey(context.Context) (httpsignature.SigningKey, error) {
	return provider.key, nil
}

type keyResolver struct{ key httpsignature.ResolvedKey }

func (resolver keyResolver) Resolve(context.Context, string) (httpsignature.ResolvedKey, error) {
	return resolver.key, nil
}

type localCandidate struct {
	signer   *httpsignature.Signer
	verifier *httpsignature.Verifier
}

type peerCandidate struct {
	signer   *peer.Signer
	verifier *peer.Verifier
}

type dadrusCandidate struct {
	signer   dadrus.Signer
	verifier dadrus.Verifier
}

func TestCandidatesSignAndVerifyEquivalentRequestCoverage(t *testing.T) {
	t.Parallel()

	request := benchmarkRequest(t)
	if err := newLocalCandidate(t).signAndVerify(request); err != nil {
		t.Fatalf("local sign and verify: %v", err)
	}

	request = benchmarkRequest(t)
	if err := newPeerCandidate(t).signAndVerify(request); err != nil {
		t.Fatalf("peer sign and verify: %v", err)
	}

	request = benchmarkRequest(t)
	if err := newDadrusCandidate(t).signAndVerify(request); err != nil {
		t.Fatalf("dadrus sign and verify: %v", err)
	}
}

func TestCandidatesDoNotAccumulateSignatureFieldsAcrossCalls(t *testing.T) {
	t.Parallel()

	t.Run("HTTPMessageSignature", func(t *testing.T) {
		candidate := newLocalCandidate(t)
		assertCandidateDoesNotAccumulateSignatures(t, candidate.signAndVerify)
	})
	t.Run("YaronFHTTPSign", func(t *testing.T) {
		candidate := newPeerCandidate(t)
		assertCandidateDoesNotAccumulateSignatures(t, candidate.signAndVerify)
	})
	t.Run("DadrusHTTPSig", func(t *testing.T) {
		candidate := newDadrusCandidate(t)
		assertCandidateDoesNotAccumulateSignatures(t, candidate.signAndVerify)
	})
}

func assertCandidateDoesNotAccumulateSignatures(t *testing.T, signAndVerify func(*http.Request) error) {
	t.Helper()

	request := benchmarkRequest(t)
	for range 2 {
		if err := signAndVerify(request); err != nil {
			t.Fatalf("sign and verify: %v", err)
		}
	}
	inputs, err := httpsignature.ParseSignatureInputs(request.Header.Values("Signature-Input"))
	if err != nil {
		t.Fatalf("parse Signature-Input: %v", err)
	}
	signatures, err := httpsignature.ParseSignatures(request.Header.Values("Signature"))
	if err != nil {
		t.Fatalf("parse Signature: %v", err)
	}
	if entries := inputs.Entries(); len(entries) != 1 || entries[0].Label != "sig" {
		t.Fatalf("Signature-Input entries = %#v, want one sig entry", entries)
	}
	if entries := signatures.Entries(); len(entries) != 1 || entries[0].Label != "sig" {
		t.Fatalf("Signature entries = %#v, want one sig entry", entries)
	}
}

func BenchmarkRequestHMACSHA256SignVerify(b *testing.B) {
	b.Run("HTTPMessageSignature", func(b *testing.B) {
		candidate := newLocalCandidate(b)
		request := benchmarkRequest(b)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			if err := candidate.signAndVerify(request); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("YaronFHTTPSign", func(b *testing.B) {
		candidate := newPeerCandidate(b)
		request := benchmarkRequest(b)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			if err := candidate.signAndVerify(request); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("DadrusHTTPSig", func(b *testing.B) {
		candidate := newDadrusCandidate(b)
		request := benchmarkRequest(b)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			if err := candidate.signAndVerify(request); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func newLocalCandidate(tb testing.TB) localCandidate {
	tb.Helper()
	key, err := httpsignature.NewHMACKey(benchmarkKey)
	if err != nil {
		tb.Fatal(err)
	}
	components := []httpsignature.ComponentIdentifier{{Name: "@method"}, {Name: "@authority"}, {Name: "content-type"}}
	signingProfile, err := httpsignature.NewSigningProfile(httpsignature.SigningProfileConfig{
		AllowedAlgorithms:  []httpsignature.Algorithm{httpsignature.HMACSHA256},
		CoveredComponents:  components,
		Expires:            httpsignature.ParameterForbidden,
		AlgorithmParameter: httpsignature.ParameterRequired,
		Nonce:              httpsignature.ParameterForbidden,
		Tag:                httpsignature.ParameterForbidden,
		ResolveTimeout:     time.Second,
		Now:                time.Now,
		Provider: signingProvider{key: httpsignature.SigningKey{
			KeyID: "benchmark-key", Algorithm: httpsignature.HMACSHA256, Key: key,
			NotBefore: time.Unix(0, 0), NotAfter: time.Unix(1<<62, 0),
		}},
	})
	if err != nil {
		tb.Fatal(err)
	}
	verificationProfile, err := httpsignature.NewVerificationProfile(httpsignature.VerificationProfileConfig{
		AllowedAlgorithms:  []httpsignature.Algorithm{httpsignature.HMACSHA256},
		RequiredComponents: components,
		Created:            httpsignature.ParameterRequired,
		Expires:            httpsignature.ParameterForbidden,
		AlgorithmParameter: httpsignature.ParameterRequired,
		Nonce:              httpsignature.ParameterForbidden,
		Tag:                httpsignature.ParameterForbidden,
		MaxAge:             time.Minute,
		ClockSkew:          time.Second,
		ResolveTimeout:     time.Second,
		Now:                time.Now,
		Resolver: keyResolver{key: httpsignature.ResolvedKey{
			Algorithm: httpsignature.HMACSHA256, Key: key,
			NotBefore: time.Unix(0, 0), NotAfter: time.Unix(1<<62, 0), FreshUntil: time.Unix(1<<62, 0),
		}},
	})
	if err != nil {
		tb.Fatal(err)
	}
	return localCandidate{signer: httpsignature.NewSigner(signingProfile), verifier: httpsignature.NewVerifier(verificationProfile)}
}

func (candidate localCandidate) signAndVerify(request *http.Request) error {
	resetSignatureFields(request)
	signed, err := candidate.signer.Sign(context.Background(), httpsignature.MessageContext{Request: request}, "sig", httpsignature.SigningOptions{})
	if err != nil {
		return err
	}
	request.Header.Set("Signature-Input", signed.SignatureInputField())
	request.Header.Set("Signature", signed.SignatureField())
	inputs, err := httpsignature.ParseSignatureInputs(request.Header.Values("Signature-Input"))
	if err != nil {
		return err
	}
	signatures, err := httpsignature.ParseSignatures(request.Header.Values("Signature"))
	if err != nil {
		return err
	}
	_, err = candidate.verifier.Verify(context.Background(), httpsignature.MessageContext{Request: request}, "sig", inputs, signatures)
	return err
}

func newPeerCandidate(tb testing.TB) peerCandidate {
	tb.Helper()
	fields := peer.Headers("@method", "@authority", "content-type")
	signer, err := peer.NewHMACSHA256Signer(benchmarkKey, peer.NewSignConfig().SetKeyID("benchmark-key"), fields)
	if err != nil {
		tb.Fatal(err)
	}
	verifier, err := peer.NewHMACSHA256Verifier(benchmarkKey, peer.NewVerifyConfig().SetKeyID("benchmark-key"), fields)
	if err != nil {
		tb.Fatal(err)
	}
	return peerCandidate{signer: signer, verifier: verifier}
}

func (candidate peerCandidate) signAndVerify(request *http.Request) error {
	resetSignatureFields(request)
	signatureInput, signature, err := peer.SignRequest("sig", *candidate.signer, request)
	if err != nil {
		return err
	}
	request.Header.Set("Signature-Input", signatureInput)
	request.Header.Set("Signature", signature)
	return peer.VerifyRequest("sig", *candidate.verifier, request)
}

func newDadrusCandidate(tb testing.TB) dadrusCandidate {
	tb.Helper()
	key := dadrus.Key{KeyID: "benchmark-key", Algorithm: dadrus.HmacSha256, Key: benchmarkKey}
	signer, err := dadrus.NewSigner(key,
		dadrus.WithLabel("sig"),
		dadrus.WithTTL(0),
		dadrus.WithNonce(dadrus.NonceGetterFunc(func(context.Context) (string, error) { return "", nil })),
		dadrus.WithComponents("@method", "@authority", "content-type"),
	)
	if err != nil {
		tb.Fatal(err)
	}
	verifier, err := dadrus.NewVerifier(key,
		dadrus.WithValidateAllSignatures(),
		dadrus.WithRequiredComponents("@method", "@authority", "content-type"),
		dadrus.WithCreatedTimestampRequired(true),
		dadrus.WithExpiredTimestampRequired(false),
		dadrus.WithMaxAge(time.Minute),
		dadrus.WithValidityTolerance(time.Second),
	)
	if err != nil {
		tb.Fatal(err)
	}
	return dadrusCandidate{signer: signer, verifier: verifier}
}

func (candidate dadrusCandidate) signAndVerify(request *http.Request) error {
	resetSignatureFields(request)
	header, err := candidate.signer.Sign(dadrus.MessageFromRequest(request))
	if err != nil {
		return err
	}
	request.Header.Set("Signature-Input", header.Get("Signature-Input"))
	request.Header.Set("Signature", header.Get("Signature"))
	return candidate.verifier.Verify(dadrus.MessageFromRequest(request))
}

func resetSignatureFields(request *http.Request) {
	request.Header.Del("Signature-Input")
	request.Header.Del("Signature")
}

func benchmarkRequest(tb testing.TB) *http.Request {
	tb.Helper()
	request, err := http.NewRequest(http.MethodPost, "https://api.example.test/payments?tenant=one", nil)
	if err != nil {
		tb.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	return request
}
