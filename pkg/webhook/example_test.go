package webhook_test

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"time"

	webhook "github.com/faustbrian/golib/pkg/webhook"
)

func ExampleVerifier_Middleware() {
	now := time.Unix(1_700_000_000, 0)
	secret := []byte("example-secret-at-least-rotate-in-production")
	signer, _ := webhook.NewSigner(webhook.SignerConfig{
		Algorithm: webhook.SHA256,
		Keys:      []webhook.SigningKey{{ID: "current", Secret: secret}},
		Clock:     func() time.Time { return now },
	})
	verifier, _ := webhook.NewVerifier(webhook.VerifierConfig{
		Algorithm: webhook.SHA256,
		Keys:      []webhook.VerificationKey{{ID: "current", Secret: secret}},
		Clock:     func() time.Time { return now }, Tolerance: time.Minute,
	})
	next := http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		body, _ := webhook.VerifiedBodyFromContext(request.Context())
		fmt.Println(string(body))
	})
	handler, _ := verifier.Middleware(webhook.MiddlewareConfig{Request: webhook.RequestOptions{
		MaxBodyBytes: 64, HeaderLimits: webhook.HeaderLimits{MaxSignatures: 1, MaxBytes: 512},
	}}, next)
	request := httptest.NewRequest(http.MethodPost, "https://receiver.example/hooks", bytes.NewBufferString("hello"))
	_, _, _ = signer.SignRequest(request, webhook.RequestOptions{MaxBodyBytes: 64, HeaderLimits: webhook.HeaderLimits{MaxSignatures: 1, MaxBytes: 512}})
	handler.ServeHTTP(httptest.NewRecorder(), request)
	// Output: hello
}

func ExampleSigner_rotation() {
	now := time.Unix(1_700_000_000, 0)
	signer, _ := webhook.NewSigner(webhook.SignerConfig{
		Algorithm: webhook.SHA512,
		Keys: []webhook.SigningKey{
			{ID: "new", Secret: []byte("new-secret"), NotBefore: now.Add(-time.Hour)},
			{ID: "old", Secret: []byte("old-secret"), NotAfter: now.Add(time.Hour)},
		},
		Clock: func() time.Time { return now },
	})
	signatures, _ := signer.Sign(webhook.Message{Timestamp: now, Method: http.MethodPost, Path: "/hooks", Host: "receiver.example"})
	fmt.Println(len(signatures))
	// Output: 2
}

func ExampleDeliverer_Deliver() {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)
	prefix := netip.MustParsePrefix("127.0.0.0/8")
	policy, _ := webhook.NewSSRFPolicy(webhook.SSRFPolicyConfig{
		Resolver:  net.DefaultResolver,
		AllowHTTP: true, AllowedPrefixes: []netip.Prefix{prefix}, MaxAddresses: 8,
	})
	client, _ := webhook.NewSecureHTTPClient(policy, time.Second)
	signer, _ := webhook.NewSigner(webhook.SignerConfig{
		Algorithm: webhook.SHA256,
		Keys:      []webhook.SigningKey{{ID: "current", Secret: []byte("example-secret")}},
	})
	identifier := 0
	deliverer, _ := webhook.NewDeliverer(webhook.DeliveryConfig{
		Client: client, Signer: signer, EndpointPolicy: policy,
		Retry: webhook.RetryPolicy{MaxAttempts: 1},
		IDGenerator: func() (string, error) {
			identifier++
			return fmt.Sprintf("id-%d", identifier), nil
		},
		MaxRequestBytes: 64, MaxResponseBytes: 64, MaxFanOut: 1,
		HeaderLimits: webhook.HeaderLimits{MaxSignatures: 1, MaxBytes: 512},
	})
	result, _ := deliverer.Deliver(context.Background(), webhook.DeliveryRequest{
		Endpoint: endpoint, Body: []byte("hello"), EventID: "event-1",
	})
	fmt.Println(result.Attempts[0].StatusCode, len(result.Attempts))
	// Output: 204 1
}

func ExampleObserver() {
	observer := webhook.ObserverFunc(func(_ context.Context, event webhook.Observation) {
		fmt.Println(event.Operation, event.Outcome)
	})
	observer.Observe(context.Background(), webhook.Observation{
		Operation: webhook.OperationVerification, Outcome: webhook.OutcomeSuccess,
	})
	// Output: verification success
}
