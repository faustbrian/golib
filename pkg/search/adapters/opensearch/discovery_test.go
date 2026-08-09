package opensearch_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
)

type discoveryTransport struct {
	mu            sync.Mutex
	discoveryBody string
	hosts         []string
}

func (transport *discoveryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	transport.hosts = append(transport.hosts, request.URL.Host)
	body := transport.discoveryBody
	if request.URL.Path == "/" {
		body = fmt.Sprintf(
			`{"name":%q,"cluster_name":"search","cluster_uuid":"cluster-a","version":{"number":"3.6.0"}}`,
			request.URL.Hostname(),
		)
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

func TestDiscoverReplacesSeedsOnlyWithExplicitlyTrustedDataNodes(t *testing.T) {
	t.Parallel()

	transport := &discoveryTransport{discoveryBody: `{
		"nodes": {
			"manager": {"roles":["cluster_manager"],"http":{"publish_address":"manager.search.internal:9200"}},
			"data-a": {"roles":["data","ingest"],"http":{"publish_address":"node-a.search.internal:9200"}},
			"data-b": {"roles":["data"],"http":{"publish_address":"10.20.30.40:9200"}}
		}
	}`}
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{"https://seed.search.internal:9200"},
		Transport: transport, TransportOwnership: adapter.TransportBorrowed,
		RequestTimeout: time.Second, MaximumResponseBytes: 8 << 10,
		Discovery: adapter.DiscoveryPolicy{
			MaximumNodes:       4,
			AllowedDNSSuffixes: []string{".search.internal"},
			AllowedCIDRs:       []netip.Prefix{netip.MustParsePrefix("10.20.0.0/16")},
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	result, err := client.Discover(t.Context())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if result.Discovered != 2 || result.Excluded != 1 {
		t.Fatalf("Discover() = %#v", result)
	}
	first, err := client.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if first.Node != "node-a.search.internal" || second.Node != "10.20.30.40" {
		t.Fatalf("discovered nodes = %q, %q", first.Node, second.Node)
	}

	transport.mu.Lock()
	defer transport.mu.Unlock()
	wantHosts := []string{
		"seed.search.internal:9200",
		"node-a.search.internal:9200",
		"10.20.30.40:9200",
	}
	if fmt.Sprint(transport.hosts) != fmt.Sprint(wantHosts) {
		t.Fatalf("request hosts = %q, want %q", transport.hosts, wantHosts)
	}
}

func TestDiscoverRejectsUntrustedOrMalformedTopologyWithoutChangingSeeds(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"untrusted host": `{"nodes":{"evil":{"roles":["data"],"http":{"publish_address":"attacker.example:9200"}}}}`,
		"credentials":    `{"nodes":{"evil":{"roles":["data"],"http":{"publish_address":"user:secret@node.search.internal:9200"}}}}`,
		"missing port":   `{"nodes":{"bad":{"roles":["data"],"http":{"publish_address":"node.search.internal"}}}}`,
		"malformed":      `{"nodes":`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			transport := &discoveryTransport{discoveryBody: body}
			client, err := adapter.New(adapter.Config{
				Endpoints: []string{"https://seed.search.internal:9200"},
				Transport: transport, TransportOwnership: adapter.TransportBorrowed,
				RequestTimeout: time.Second, MaximumResponseBytes: 8 << 10,
				Discovery: adapter.DiscoveryPolicy{
					MaximumNodes: 2, AllowedDNSSuffixes: []string{".search.internal"},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Close() })

			if _, err := client.Discover(t.Context()); !errors.Is(err, adapter.ErrDiscoveryRejected) {
				t.Fatalf("Discover() error = %v", err)
			}
			if _, err := client.Info(t.Context()); err != nil {
				t.Fatalf("seed Info() after rejected discovery error = %v", err)
			}
			transport.mu.Lock()
			last := transport.hosts[len(transport.hosts)-1]
			transport.mu.Unlock()
			if last != "seed.search.internal:9200" {
				t.Fatalf("request used rejected host %q", last)
			}
		})
	}
}

func TestDiscoverRequiresAnExplicitBoundedTrustPolicy(t *testing.T) {
	t.Parallel()

	valid := adapter.Config{
		Endpoints: []string{"https://seed.search.internal:9200"},
		Transport: &discoveryTransport{}, TransportOwnership: adapter.TransportBorrowed,
		RequestTimeout: time.Second, MaximumResponseBytes: 8 << 10,
	}
	client, err := adapter.New(valid)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if _, err := client.Discover(t.Context()); !errors.Is(err, adapter.ErrDiscoveryDisabled) {
		t.Fatalf("Discover() error = %v", err)
	}

	valid.Discovery = adapter.DiscoveryPolicy{MaximumNodes: adapter.MaximumDiscoveredNodes + 1}
	if client, err := adapter.New(valid); client != nil || !errors.Is(err, adapter.ErrInvalidConfig) {
		t.Fatalf("New() client/error = %#v/%v", client, err)
	}
	valid.Discovery = adapter.DiscoveryPolicy{MaximumNodes: 2}
	if client, err := adapter.New(valid); client != nil || !errors.Is(err, adapter.ErrInvalidConfig) {
		t.Fatalf("New() without allowlist client/error = %#v/%v", client, err)
	}

	//lint:ignore SA1012 Explicit nil-context validation is the contract under test.
	if _, err := client.Discover(nil); !errors.Is(err, adapter.ErrContextRequired) { //nolint:staticcheck // nil-context validation is the contract under test.
		t.Fatalf("Discover(nil) error = %v", err)
	}
	if _, err := client.Discover(context.Background()); !errors.Is(err, adapter.ErrDiscoveryDisabled) {
		t.Fatalf("Discover() disabled error = %v", err)
	}
}
