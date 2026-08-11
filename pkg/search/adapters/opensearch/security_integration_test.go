//go:build integration

package opensearch_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
)

type secureCredentialProvider struct {
	mu       sync.RWMutex
	username string
	password string
}

func (provider *secureCredentialProvider) Credentials(context.Context) (adapter.BasicCredentials, error) {
	provider.mu.RLock()
	defer provider.mu.RUnlock()
	return adapter.BasicCredentials{Username: provider.username, Password: provider.password}, nil
}

func (provider *secureCredentialProvider) setPassword(password string) {
	provider.mu.Lock()
	provider.password = password
	provider.mu.Unlock()
}

type secureDialTarget struct{ value atomic.Value }

func newSecureDialTarget(address string) *secureDialTarget {
	target := &secureDialTarget{}
	target.value.Store(address)
	return target
}

func (target *secureDialTarget) set(address string) { target.value.Store(address) }

func (target *secureDialTarget) dial(ctx context.Context, network, _ string) (net.Conn, error) {
	return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, target.value.Load().(string))
}

func TestRealOpenSearchSecureTLSCredentialRotationDNSAndRecovery(t *testing.T) {
	serverName := os.Getenv("OPENSEARCH_TLS_SERVER_NAME")
	firstAddress := os.Getenv("OPENSEARCH_FIRST_DIAL_ADDRESS")
	secondAddress := os.Getenv("OPENSEARCH_SECOND_DIAL_ADDRESS")
	caFile := os.Getenv("OPENSEARCH_CA_FILE")
	username := os.Getenv("OPENSEARCH_OPERATOR_USERNAME")
	oldPassword := os.Getenv("OPENSEARCH_OLD_PASSWORD")
	newPassword := os.Getenv("OPENSEARCH_NEW_PASSWORD")
	expectedVersion := os.Getenv("OPENSEARCH_EXPECTED_VERSION")
	if serverName == "" || firstAddress == "" || secondAddress == "" || caFile == "" || username == "" || oldPassword == "" || newPassword == "" || expectedVersion == "" {
		t.Skip("secure real-cluster environment is not configured")
	}
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("OpenSearch CA file contains no certificate")
	}
	provider := &secureCredentialProvider{username: username, password: oldPassword}
	target := newSecureDialTarget(firstAddress)
	transport := secureIntegrationTransport(roots, serverName, target)
	client := secureIntegrationClient(t, serverName, provider, transport)
	first, err := client.Info(t.Context())
	if err != nil || first.Version != expectedVersion {
		t.Fatalf("secure first Info() = %#v/%v", first, err)
	}

	wrongRoots := x509.NewCertPool()
	wrongTransport := secureIntegrationTransport(wrongRoots, serverName, newSecureDialTarget(firstAddress))
	wrongTLSClient := secureIntegrationClient(t, serverName, provider, wrongTransport)
	if _, infoErr := wrongTLSClient.Info(t.Context()); !errors.Is(infoErr, adapter.ErrTransport) {
		t.Fatalf("untrusted TLS Info() error = %v", infoErr)
	}

	httpClient := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	rotateBody, err := json.Marshal(map[string]string{"current_password": oldPassword, "password": newPassword})
	if err != nil {
		t.Fatal(err)
	}
	secureDirectRequest(t, httpClient, username, oldPassword, http.MethodPut, "https://"+serverName+"/_plugins/_security/api/account", rotateBody, http.StatusOK)
	provider.setPassword(newPassword)
	rotated, err := client.Info(t.Context())
	if err != nil || rotated.ClusterUUID != first.ClusterUUID {
		t.Fatalf("rotated credential Info() = %#v/%v", rotated, err)
	}

	target.set(secondAddress)
	transport.CloseIdleConnections()
	second, err := client.Info(t.Context())
	if err != nil || second.Version != expectedVersion || second.ClusterUUID == first.ClusterUUID {
		t.Fatalf("DNS-changed Info() = %#v/%v", second, err)
	}
	baselineHealth, err := client.Health(t.Context())
	if err != nil || !baselineHealth.Ready {
		t.Fatalf("secure baseline Health() = %#v/%v", baselineHealth, err)
	}
	capacity, err := client.Capacity(t.Context())
	if err != nil || capacity.Nodes < 1 {
		t.Fatalf("secure operator Capacity() = %#v/%v", capacity, err)
	}

	fixture := "golib-secure-recovery-" + strings.ReplaceAll(time.Now().UTC().Format("150405.000000000"), ".", "")
	secureDirectRequest(t, httpClient, username, newPassword, http.MethodPut, "https://"+serverName+"/"+fixture,
		[]byte(`{"settings":{"number_of_shards":1,"number_of_replicas":1},"mappings":{"properties":{"value":{"type":"keyword"}}}}`), http.StatusOK)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodDelete, "https://"+serverName+"/"+fixture, nil)
		if requestErr == nil {
			request.SetBasicAuth(username, newPassword)
			response, responseErr := httpClient.Do(request)
			if responseErr == nil && response != nil {
				_, _ = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
			}
		}
	})
	waitForSecureHealth(t, client, func(report adapter.HealthReport) bool {
		return report.Ready && report.UnassignedShards > baselineHealth.UnassignedShards
	})
	secureDirectRequest(t, httpClient, username, newPassword, http.MethodPut, "https://"+serverName+"/"+fixture+"/_settings",
		[]byte(`{"index":{"number_of_replicas":0}}`), http.StatusOK)
	waitForSecureHealth(t, client, func(report adapter.HealthReport) bool {
		return report.Status == baselineHealth.Status && report.Ready && report.UnassignedShards == baselineHealth.UnassignedShards
	})
}

func TestRealOpenSearchLeastPrivilegeTenantIsolation(t *testing.T) {
	serverName := os.Getenv("OPENSEARCH_TLS_SERVER_NAME")
	address := os.Getenv("OPENSEARCH_FIRST_DIAL_ADDRESS")
	caFile := os.Getenv("OPENSEARCH_CA_FILE")
	username := os.Getenv("OPENSEARCH_USERNAME")
	password := os.Getenv("OPENSEARCH_OLD_PASSWORD")
	tenantAAlias := os.Getenv("OPENSEARCH_TENANT_A_ALIAS")
	tenantAPhysical := os.Getenv("OPENSEARCH_TENANT_A_PHYSICAL")
	tenantBPhysical := os.Getenv("OPENSEARCH_TENANT_B_PHYSICAL")
	if serverName == "" || address == "" || caFile == "" || username == "" || password == "" ||
		tenantAAlias == "" || tenantAPhysical == "" || tenantBPhysical == "" {
		t.Skip("least-privilege real-cluster environment is not configured")
	}
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		t.Fatal("OpenSearch CA file contains no certificate")
	}
	provider := &secureCredentialProvider{username: username, password: password}
	transport := secureIntegrationTransport(roots, serverName, newSecureDialTarget(address))
	httpClient := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	probeBody := secureDirectRequest(t, httpClient, username, password, http.MethodPut,
		"https://"+serverName+"/"+tenantAAlias+"/_doc/probe?version=1&version_type=external&refresh=wait_for&require_alias=true",
		[]byte(`{"value":"probe"}`), http.StatusCreated)
	var probe struct {
		Index   string `json:"_index"`
		ID      string `json:"_id"`
		Version uint64 `json:"_version"`
		Result  string `json:"result"`
	}
	if json.Unmarshal(probeBody, &probe) != nil || probe.Index != tenantAPhysical || probe.ID != "probe" || probe.Version != 1 || probe.Result != "created" {
		t.Fatalf("tenant-a direct write attribution = %#v", probe)
	}
	client := secureRuntimeSearchClient(t, serverName, provider, transport, tenantAAlias, tenantAPhysical)
	document, err := search.NewDocument("tenant-a", "documents", "allowed", 1, json.RawMessage(`{"value":"allowed"}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if outcome, writeErr := client.Write(t.Context(), search.IndexDocument(document), search.RefreshWaitFor); writeErr != nil || outcome.State != search.OutcomeApplied {
		t.Fatalf("tenant-a Write() = %#v/%v", outcome, writeErr)
	}
	result, err := client.Search(t.Context(), search.Request{
		Tenant: "tenant-a", Index: "documents", Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}}, Page: search.OffsetPage{Size: 10},
	})
	if err != nil || len(result.Hits()) != 2 || result.Hits()[0].ID != "allowed" || result.Hits()[1].ID != "probe" {
		t.Fatalf("tenant-a Search() = %#v/%v", result.Hits(), err)
	}

	for name, test := range map[string]struct {
		method string
		path   string
		body   []byte
	}{
		"tenant-b read":  {method: http.MethodGet, path: "/" + tenantBPhysical + "/_search"},
		"tenant-b write": {method: http.MethodPut, path: "/" + tenantBPhysical + "/_doc/denied", body: []byte(`{"value":"denied"}`)},
		"cluster health": {method: http.MethodGet, path: "/_cluster/health"},
		"security admin": {method: http.MethodGet, path: "/_plugins/_security/api/roles"},
	} {
		test := test
		t.Run(name, func(t *testing.T) {
			status := secureDirectStatus(t, httpClient, username, password, test.method, "https://"+serverName+test.path, test.body)
			if status != http.StatusForbidden {
				t.Fatalf("forbidden boundary status = %d, want 403", status)
			}
		})
	}
}

func secureIntegrationTransport(roots *x509.CertPool, serverName string, target *secureDialTarget) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = target.dial
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: serverName}
	return transport
}

func secureIntegrationClient(t *testing.T, serverName string, provider adapter.BasicCredentialsProvider, transport *http.Transport) *adapter.Client {
	t.Helper()
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{"https://" + serverName}, BasicCredentials: provider,
		Transport: transport, TransportOwnership: adapter.TransportBorrowed,
		RequestTimeout: 10 * time.Second, MaximumResponseBytes: 16 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		transport.CloseIdleConnections()
	})
	return client
}

func secureRuntimeSearchClient(t *testing.T, serverName string, provider adapter.BasicCredentialsProvider, transport *http.Transport, alias, physical string) *adapter.Client {
	t.Helper()
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{"https://" + serverName}, BasicCredentials: provider,
		Transport: transport, TransportOwnership: adapter.TransportBorrowed,
		RequestTimeout: 10 * time.Second, MaximumResponseBytes: 16 << 20,
		Search: &adapter.SearchConfig{
			Limits: search.DefaultLimits(), CursorCodec: mustCursorCodec(t), Clock: time.Now,
			Authorizer: allowSearchAuthorization(), WriteGuard: allowWriteAuthorization(),
			Resolver: adapter.IndexResolverFunc(func(_ context.Context, tenant, index string, _ adapter.IndexAccess) (adapter.IndexTarget, error) {
				if tenant != "tenant-a" || index != "documents" {
					return adapter.IndexTarget{}, errors.New("tenant target denied")
				}
				return adapter.IndexTarget{Name: alias, PhysicalName: physical, Fingerprint: "secure-v1"}, nil
			}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		transport.CloseIdleConnections()
	})
	return client
}

func secureDirectRequest(t *testing.T, client *http.Client, username, password, method, target string, body []byte, expectedStatus int) []byte {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, target, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.SetBasicAuth(username, password)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal("secure OpenSearch request failed")
	}
	if response == nil {
		t.Fatal("secure OpenSearch request returned no response")
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if readErr != nil || len(responseBody) > 1<<20 || response.StatusCode != expectedStatus {
		t.Fatalf("secure OpenSearch request status/read/bytes/action = %d/%v/%d/%q", response.StatusCode, readErr, len(responseBody), deniedSecurityAction(responseBody))
	}
	return responseBody
}

func deniedSecurityAction(body []byte) string {
	match := regexp.MustCompile(`no permissions for \[([^]]+)]`).FindSubmatch(body)
	if len(match) != 2 {
		return "unknown"
	}
	return string(match[1])
}

func secureDirectStatus(t *testing.T, client *http.Client, username, password, method, target string, body []byte) int {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, target, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.SetBasicAuth(username, password)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal("secure OpenSearch request failed")
	}
	if response == nil {
		t.Fatal("secure OpenSearch request returned no response")
	}
	defer func() { _ = response.Body.Close() }()
	if _, err := io.Copy(io.Discard, io.LimitReader(response.Body, (1<<20)+1)); err != nil {
		t.Fatal("secure OpenSearch response read failed")
	}
	return response.StatusCode
}

func waitForSecureHealth(t *testing.T, client *adapter.Client, ready func(adapter.HealthReport) bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		report, err := client.Health(t.Context())
		if err == nil && ready(report) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("secure health transition did not complete: %#v/%v", report, err)
		}
		select {
		case <-time.After(100 * time.Millisecond):
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		}
	}
}
