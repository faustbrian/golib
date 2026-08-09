package opensearch_test

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
	"github.com/opensearch-project/opensearch-go/v4/signer"
	"github.com/opensearch-project/opensearch-go/v4/signer/awsv2"
)

type rotatingBasicCredentials struct {
	mu        sync.Mutex
	passwords []string
	next      int
}

type rotatingAWSCredentials struct {
	mu   sync.Mutex
	next int
}

func (provider *rotatingAWSCredentials) Retrieve(context.Context) (aws.Credentials, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.next++

	return aws.Credentials{
		AccessKeyID:     fmt.Sprintf("AKIDROTATION%d", provider.next),
		SecretAccessKey: "synthetic-secret-access-key",
		SessionToken:    fmt.Sprintf("session-%d", provider.next),
		Source:          "test",
	}, nil
}

func (provider *rotatingBasicCredentials) Credentials(
	context.Context,
) (adapter.BasicCredentials, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()

	if provider.next >= len(provider.passwords) {
		return adapter.BasicCredentials{}, errors.New("credentials exhausted")
	}
	credentials := adapter.BasicCredentials{
		Username: "search-writer",
		Password: provider.passwords[provider.next],
	}
	provider.next++

	return credentials, nil
}

type inertSigner struct{}

func (inertSigner) SignRequest(*http.Request) error { return nil }
func (inertSigner) OverrideSigningPort(uint16)      {}

var _ signer.Signer = inertSigner{}

type rotatingSigner struct {
	mu   sync.Mutex
	next int
	fail bool
}

func (signer *rotatingSigner) SignRequest(request *http.Request) error {
	signer.mu.Lock()
	defer signer.mu.Unlock()
	if signer.fail {
		return errors.New("synthetic signer failure")
	}
	signer.next++
	request.Header.Set("X-Synthetic-Signature", fmt.Sprintf("signature-%d", signer.next))

	return nil
}

func (*rotatingSigner) OverrideSigningPort(uint16) {}

func TestClientUsesRotatedBasicCredentialsForEveryRequest(t *testing.T) {
	t.Parallel()

	var (
		mu      sync.Mutex
		headers []string
	)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mu.Lock()
		headers = append(headers, request.Header.Get("Authorization"))
		mu.Unlock()

		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"name":"node-a","cluster_name":"search","cluster_uuid":"cluster-a","version":{"number":"3.6.0"},"tagline":"The OpenSearch Project: https://opensearch.org/"}`)
	}))
	t.Cleanup(server.Close)

	transport := server.Client().Transport.(*http.Transport).Clone()
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{server.URL},
		BasicCredentials: &rotatingBasicCredentials{
			passwords: []string{"first-secret", "second-secret"},
		},
		Transport:            transport,
		TransportOwnership:   adapter.TransportBorrowed,
		RequestTimeout:       time.Second,
		MaximumResponseBytes: 4 << 10,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	for range 2 {
		info, infoErr := client.Info(t.Context())
		if infoErr != nil {
			t.Fatalf("Info() error = %v", infoErr)
		}
		if info.Version != "3.6.0" || info.Cluster != "search" {
			t.Fatalf("Info() = %#v", info)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{
		"Basic c2VhcmNoLXdyaXRlcjpmaXJzdC1zZWNyZXQ=",
		"Basic c2VhcmNoLXdyaXRlcjpzZWNvbmQtc2VjcmV0",
	}
	if fmt.Sprint(headers) != fmt.Sprint(want) {
		t.Fatalf("Authorization headers = %q, want %q", headers, want)
	}
}

func TestClientSelectsConfiguredNodesWithoutImplicitRetries(t *testing.T) {
	t.Parallel()

	servers := make([]*httptest.Server, 0, 2)
	for _, node := range []string{"node-a", "node-b"} {
		node := node
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprintf(writer, `{"name":%q,"cluster_name":"search","cluster_uuid":"cluster-a","version":{"number":"3.6.0"}}`, node)
		}))
		servers = append(servers, server)
		t.Cleanup(server.Close)
	}

	client, err := adapter.New(adapter.Config{
		Endpoints:         []string{servers[0].URL, servers[1].URL},
		AllowInsecureHTTP: true, RequestTimeout: time.Second,
		MaximumResponseBytes: 4 << 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	for index, want := range []string{"node-a", "node-b", "node-a"} {
		info, infoErr := client.Info(t.Context())
		if infoErr != nil {
			t.Fatalf("Info() %d error = %v", index, infoErr)
		}
		if info.Node != want {
			t.Fatalf("Info() %d node = %q, want %q", index, info.Node, want)
		}
	}
}

func TestClientSignsEveryRequestAndRedactsSignerFailure(t *testing.T) {
	t.Parallel()

	var (
		mu      sync.Mutex
		headers []string
	)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mu.Lock()
		headers = append(headers, request.Header.Get("X-Synthetic-Signature"))
		mu.Unlock()
		_, _ = io.WriteString(writer, `{"name":"node-a","cluster_name":"search","cluster_uuid":"cluster-a","version":{"number":"3.6.0"}}`)
	}))
	t.Cleanup(server.Close)

	signer := &rotatingSigner{}
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{server.URL}, Signer: signer,
		Transport:          server.Client().Transport,
		TransportOwnership: adapter.TransportBorrowed,
		RequestTimeout:     time.Second, MaximumResponseBytes: 4 << 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	for range 2 {
		if _, err := client.Info(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	if fmt.Sprint(headers) != "[signature-1 signature-2]" {
		t.Fatalf("signatures = %q", headers)
	}
	mu.Unlock()

	signer.mu.Lock()
	signer.fail = true
	signer.mu.Unlock()
	if _, err := client.Info(t.Context()); !errors.Is(err, adapter.ErrTransport) ||
		strings.Contains(err.Error(), "synthetic") {
		t.Fatalf("Info() signer error = %v", err)
	}
}

func TestOfficialAWSSignerRetrievesRotatedCredentialsForEveryRequest(t *testing.T) {
	t.Parallel()

	var (
		mu             sync.Mutex
		authorizations []string
		tokens         []string
	)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mu.Lock()
		authorizations = append(authorizations, request.Header.Get("Authorization"))
		tokens = append(tokens, request.Header.Get("X-Amz-Security-Token"))
		mu.Unlock()
		_, _ = io.WriteString(writer, `{"name":"node-a","cluster_name":"search","cluster_uuid":"cluster-a","version":{"number":"3.6.0"}}`)
	}))
	t.Cleanup(server.Close)

	provider := &rotatingAWSCredentials{}
	requestSigner, err := awsv2.NewSigner(aws.Config{
		Region: "eu-north-1", Credentials: provider,
	})
	if err != nil {
		t.Fatalf("awsv2.NewSigner() error = %v", err)
	}
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{server.URL}, Signer: requestSigner,
		Transport:          server.Client().Transport,
		TransportOwnership: adapter.TransportBorrowed,
		RequestTimeout:     time.Second, MaximumResponseBytes: 4 << 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	for range 2 {
		if _, err := client.Info(t.Context()); err != nil {
			t.Fatal(err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(authorizations) != 2 ||
		!strings.Contains(authorizations[0], "Credential=AKIDROTATION1/") ||
		!strings.Contains(authorizations[1], "Credential=AKIDROTATION2/") ||
		fmt.Sprint(tokens) != "[session-1 session-2]" {
		t.Fatalf("AWS authorization rotation = %q / %q", authorizations, tokens)
	}
}

func TestNewRejectsUnsafeOrAmbiguousTransportConfiguration(t *testing.T) {
	t.Parallel()

	valid := adapter.Config{
		Endpoints:            []string{"https://search.example.test"},
		RequestTimeout:       time.Second,
		MaximumResponseBytes: 4 << 10,
	}
	tests := []struct {
		name   string
		mutate func(*adapter.Config)
		want   error
	}{
		{"endpoint required", func(config *adapter.Config) { config.Endpoints = nil }, adapter.ErrInvalidConfig},
		{"endpoint count bounded", func(config *adapter.Config) {
			config.Endpoints = make([]string, adapter.MaximumEndpoints+1)
			for index := range config.Endpoints {
				config.Endpoints[index] = fmt.Sprintf("https://node-%d.example.test", index)
			}
		}, adapter.ErrInvalidConfig},
		{"HTTPS required", func(config *adapter.Config) { config.Endpoints = []string{"http://search.example.test"} }, adapter.ErrUnsafeEndpoint},
		{"userinfo forbidden", func(config *adapter.Config) { config.Endpoints = []string{"https://user:secret@search.example.test"} }, adapter.ErrUnsafeEndpoint},
		{"path forbidden", func(config *adapter.Config) { config.Endpoints = []string{"https://search.example.test/prefix"} }, adapter.ErrUnsafeEndpoint},
		{"duplicate endpoint", func(config *adapter.Config) {
			config.Endpoints = []string{"https://search.example.test", "https://search.example.test/"}
		}, adapter.ErrInvalidConfig},
		{"request timeout required", func(config *adapter.Config) { config.RequestTimeout = 0 }, adapter.ErrInvalidConfig},
		{"response bound required", func(config *adapter.Config) { config.MaximumResponseBytes = 0 }, adapter.ErrInvalidConfig},
		{"response bound capped", func(config *adapter.Config) { config.MaximumResponseBytes = adapter.MaximumDecodedResponseBytes + 1 }, adapter.ErrInvalidConfig},
		{"auth is exclusive", func(config *adapter.Config) {
			config.BasicCredentials = &rotatingBasicCredentials{passwords: []string{"secret"}}
			config.Signer = inertSigner{}
		}, adapter.ErrInvalidConfig},
		{"explicit proxy URL required", func(config *adapter.Config) {
			config.Proxy = adapter.ProxyPolicy{Mode: adapter.ProxyExplicit}
		}, adapter.ErrInvalidConfig},
		{"proxy credentials forbidden", func(config *adapter.Config) {
			config.Proxy = adapter.ProxyPolicy{
				Mode: adapter.ProxyExplicit,
				URL:  mustURL(t, "https://user:secret@proxy.example.test"),
			}
		}, adapter.ErrUnsafeProxy},
		{"insecure TLS forbidden", func(config *adapter.Config) {
			config.TLS = &tls.Config{InsecureSkipVerify: true}
		}, adapter.ErrInvalidTLS},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			config := valid
			test.mutate(&config)
			client, err := adapter.New(config)
			if client != nil || !errors.Is(err, test.want) {
				t.Fatalf("New() client/error = %#v/%v, want nil/%v", client, err, test.want)
			}
		})
	}
	valid.MaximumResponseBytes = adapter.MaximumDecodedResponseBytes
	client, err := adapter.New(valid)
	if err != nil {
		t.Fatalf("New() exact maximum response bound error = %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestExplicitInsecureHTTPCannotCarryCredentials(t *testing.T) {
	t.Parallel()

	config := adapter.Config{
		Endpoints:            []string{"http://127.0.0.1:9200"},
		AllowInsecureHTTP:    true,
		RequestTimeout:       time.Second,
		MaximumResponseBytes: 4 << 10,
		BasicCredentials: &rotatingBasicCredentials{
			passwords: []string{"secret"},
		},
	}
	client, err := adapter.New(config)
	if client != nil || !errors.Is(err, adapter.ErrUnsafeEndpoint) {
		t.Fatalf("New() client/error = %#v/%v", client, err)
	}

	config.BasicCredentials = nil
	client, err = adapter.New(config)
	if err != nil {
		t.Fatalf("New() explicit HTTP error = %v", err)
	}
	if closeErr := client.Close(); closeErr != nil {
		t.Fatalf("Close() error = %v", closeErr)
	}
}

func TestBorrowedTransportRemainsCallerOwned(t *testing.T) {
	t.Parallel()

	transport := &observedTransport{}
	client, err := adapter.New(adapter.Config{
		Endpoints:            []string{"https://search.example.test"},
		Transport:            transport,
		TransportOwnership:   adapter.TransportBorrowed,
		RequestTimeout:       time.Second,
		MaximumResponseBytes: 4 << 10,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if transport.closed != 0 {
		t.Fatalf("borrowed transport close count = %d", transport.closed)
	}
}

func TestOwnedTransportIsUsedAndClosedExactlyOnce(t *testing.T) {
	t.Parallel()

	transport := &observedTransport{response: validInfoResponse("owned-node")}
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{"https://search.example.test"},
		Transport: transport, TransportOwnership: adapter.TransportOwned,
		RequestTimeout: time.Second, MaximumResponseBytes: 4 << 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	info, err := client.Info(t.Context())
	if err != nil || info.Node != "owned-node" {
		t.Fatalf("Info() = %#v, %v", info, err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if transport.closed != 1 || transport.requests != 1 {
		t.Fatalf("transport close/request counts = %d/%d", transport.closed, transport.requests)
	}
	if _, err := client.Info(t.Context()); !errors.Is(err, adapter.ErrClosed) {
		t.Fatalf("Info() after Close error = %v", err)
	}
}

type observedTransport struct {
	closed   int
	requests int
	response *http.Response
	err      error
}

func (transport *observedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	transport.requests++

	return transport.response, transport.err
}

func (transport *observedTransport) CloseIdleConnections() { transport.closed++ }

func mustURL(t *testing.T, value string) *url.URL {
	t.Helper()

	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}

	return parsed
}

func TestInfoRejectsMalformedAndOversizedResponsesWithoutLeakingBodies(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body string
		want error
	}{
		"malformed": {body: `{"version":`, want: adapter.ErrMalformedResponse},
		"oversized": {body: strings.Repeat("x", 65), want: adapter.ErrResponseTooLarge},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(writer, test.body)
			}))
			t.Cleanup(server.Close)
			client, err := adapter.New(adapter.Config{
				Endpoints:            []string{server.URL},
				Transport:            server.Client().Transport,
				TransportOwnership:   adapter.TransportBorrowed,
				RequestTimeout:       time.Second,
				MaximumResponseBytes: 64,
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Close() })

			if _, err := client.Info(t.Context()); !errors.Is(err, test.want) {
				t.Fatalf("Info() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestInfoRequiresContextAndClassifiesTransportAndStatusFailures(t *testing.T) {
	t.Parallel()

	client, err := adapter.New(adapter.Config{
		Endpoints:          []string{"https://search.example.test"},
		Transport:          &observedTransport{err: errors.New("dial detail")},
		TransportOwnership: adapter.TransportBorrowed,
		RequestTimeout:     time.Second, MaximumResponseBytes: 4 << 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	//lint:ignore SA1012 Explicit nil-context validation is the contract under test.
	if _, err := client.Info(nil); !errors.Is(err, adapter.ErrContextRequired) { //nolint:staticcheck // nil-context validation is the contract under test.
		t.Fatalf("Info(nil) error = %v", err)
	}
	if _, err := client.Info(t.Context()); !errors.Is(err, adapter.ErrTransport) ||
		strings.Contains(err.Error(), "dial detail") {
		t.Fatalf("Info() transport error = %v", err)
	}

	statusClient, err := adapter.New(adapter.Config{
		Endpoints: []string{"https://search.example.test"},
		Transport: &observedTransport{response: &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Body:       io.NopCloser(strings.NewReader(`{"error":"detail"}`)),
		}},
		TransportOwnership: adapter.TransportBorrowed,
		RequestTimeout:     time.Second, MaximumResponseBytes: 4 << 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = statusClient.Close() })
	if _, err := statusClient.Info(t.Context()); !errors.Is(err, adapter.ErrUnexpectedStatus) ||
		!strings.Contains(err.Error(), "HTTP 429") || strings.Contains(err.Error(), "detail") {
		t.Fatalf("Info() status error = %v", err)
	}
}

func validInfoResponse(node string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(fmt.Sprintf(
			`{"name":%q,"cluster_name":"search","cluster_uuid":"cluster-a","version":{"number":"3.6.0"}}`,
			node,
		))),
		Header: make(http.Header),
	}
}
