package httpclient

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestDefaultEgressPolicyAllowsPublicHTTPSAndDeniesUnsafeClasses(t *testing.T) {
	t.Parallel()

	policy, err := NewEgressPolicy(EgressOptions{})
	if err != nil {
		t.Fatalf("construct default egress: %v", err)
	}
	allowed, _ := url.Parse("https://203.0.113.10/resource?secret=value")
	if err := policy.ValidateURL(allowed); err != nil {
		t.Fatalf("public HTTPS denied: %v", err)
	}
	for _, rawURL := range []string{
		"http://203.0.113.10/",
		"https://203.0.113.10:8443/",
		"https://127.0.0.1/",
		"https://10.0.0.1/",
		"https://169.254.1.1/",
		"https://224.0.0.1/",
		"https://169.254.169.254/latest/meta-data/",
	} {
		target, _ := url.Parse(rawURL)
		err := policy.ValidateURL(target)
		var egressError *EgressError
		if !errors.As(err, &egressError) || !errors.Is(err, ErrEgressDenied) ||
			target.RawQuery != "" && strings.Contains(err.Error(), target.RawQuery) {
			t.Fatalf("unsafe target %q error = %#v", rawURL, err)
		}
	}
}

func TestEgressPolicyAppliesOriginHostPortAndCIDRAllowlists(t *testing.T) {
	t.Parallel()

	policy, err := NewEgressPolicy(EgressOptions{
		AllowedSchemes: []string{"http"},
		AllowedHosts:   []string{"api.example.test"},
		AllowedPorts:   []uint16{8080},
		AllowedOrigins: []string{"http://api.example.test:8080"},
		AllowedCIDRs:   []string{"192.0.2.0/24"},
	})
	if err != nil {
		t.Fatalf("construct allowlisted egress: %v", err)
	}
	target, _ := url.Parse("http://api.example.test:8080/widgets")
	if err := policy.ValidateURL(target); err != nil {
		t.Fatalf("allowlisted URL denied: %v", err)
	}
	if err := policy.ValidateIP(net.ParseIP("192.0.2.10")); err != nil {
		t.Fatalf("allowlisted IP denied: %v", err)
	}
	for _, rawURL := range []string{
		"https://api.example.test:8080/",
		"http://other.example.test:8080/",
		"http://api.example.test:8081/",
	} {
		target, _ := url.Parse(rawURL)
		if err := policy.ValidateURL(target); !errors.Is(err, ErrEgressDenied) {
			t.Fatalf("URL %q error = %v", rawURL, err)
		}
	}
	if err := policy.ValidateIP(net.ParseIP("198.51.100.10")); !errors.Is(err, ErrEgressDenied) {
		t.Fatalf("outside CIDR error = %v", err)
	}
}

func TestEgressPolicyAllowsExplicitAddressClasses(t *testing.T) {
	t.Parallel()

	policy, err := NewEgressPolicy(EgressOptions{
		AllowedSchemes: []string{"http"}, AllowedPorts: []uint16{80},
		AllowPrivate: true, AllowLoopback: true, AllowLinkLocal: true,
		AllowMulticast: true, AllowMetadataService: true,
	})
	if err != nil {
		t.Fatalf("construct class policy: %v", err)
	}
	for _, address := range []string{"10.0.0.1", "127.0.0.1", "169.254.1.1", "224.0.0.1", "169.254.169.254"} {
		if err := policy.ValidateIP(net.ParseIP(address)); err != nil {
			t.Fatalf("address %s denied: %v", address, err)
		}
	}
}

func TestClientEgressEnforcesDefaultTransportAndRedirects(t *testing.T) {
	t.Parallel()

	if _, err := NewEgressPolicy(EgressOptions{AllowedSchemes: []string{"ftp"}}); !errors.Is(err, ErrInvalidEgressPolicy) {
		t.Fatalf("invalid scheme error = %v", err)
	}
	policy, _ := NewEgressPolicy(EgressOptions{})
	if _, err := New(Config{Transport: TransportFunc(func(*http.Request) (*http.Response, error) {
		return nil, nil
	}), Egress: policy}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("custom transport egress error = %v", err)
	}

	client, err := New(Config{Egress: policy})
	if err != nil {
		t.Fatalf("construct egress client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.test/", nil)
	if _, err := client.Do(request); !errors.Is(err, ErrEgressDenied) {
		t.Fatalf("initial egress error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Redirect(writer, &http.Request{}, "http://169.254.169.254/latest", http.StatusFound)
	}))
	t.Cleanup(server.Close)
	serverURL, _ := url.Parse(server.URL)
	port, _ := strconv.ParseUint(serverURL.Port(), 10, 16)
	localPolicy, err := NewEgressPolicy(EgressOptions{
		AllowedSchemes: []string{"http"}, AllowedHosts: []string{serverURL.Hostname()},
		AllowedPorts: []uint16{uint16(port)}, AllowLoopback: true,
	})
	if err != nil {
		t.Fatalf("construct local policy: %v", err)
	}
	localClient, err := New(Config{Egress: localPolicy})
	if err != nil {
		t.Fatalf("construct local client: %v", err)
	}
	t.Cleanup(func() { _ = localClient.Close() })
	request, _ = http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if _, err := localClient.Do(request); !errors.Is(err, ErrEgressDenied) {
		t.Fatalf("redirect egress error = %v", err)
	}
}

func TestClientEgressPreservesAllowedRedirectPolicy(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/start" {
			http.Redirect(writer, request, "/final", http.StatusFound)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	serverURL, _ := url.Parse(server.URL)
	port, _ := strconv.ParseUint(serverURL.Port(), 10, 16)
	policy, _ := NewEgressPolicy(EgressOptions{
		AllowedSchemes: []string{"http"}, AllowedHosts: []string{serverURL.Hostname()},
		AllowedPorts: []uint16{uint16(port)}, AllowLoopback: true,
	})
	for _, configured := range []bool{false, true} {
		client, err := New(Config{Egress: policy})
		if err != nil {
			t.Fatalf("construct redirect client: %v", err)
		}
		if configured {
			client.HTTPClient().CheckRedirect = func(*http.Request, []*http.Request) error { return nil }
		}
		request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/start", nil)
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("allowed redirect configured %t: %v", configured, err)
		}
		_ = response.Body.Close()
		_ = client.Close()
	}
}
