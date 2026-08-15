//go:build integration

package opensearch_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
)

func TestRealOpenSearchMixedApplicationProtocolVersions(t *testing.T) {
	endpoint, direct := realFaultEnvironment(t)
	limits := search.DefaultLimits()
	tenant, logicalIndex := "mixed-application-tenant", "documents"
	physical, alias := realFaultNames("mixed-application")
	createRealFaultIndex(t, direct, physical, alias,
		`{"dynamic":"strict","properties":{"writer":{"type":"keyword"},"sequence":{"type":"long"}}}`)
	current := newBoundIntegrationSearchClient(t, endpoint, tenant, logicalIndex, alias, physical, "mixed-application-definition-v1", limits)

	peerBinary := buildFrozenMixedApplicationPeer(t)

	ready := make(chan struct{}, 1)
	start := make(chan struct{})
	barrier := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		select {
		case ready <- struct{}{}:
		default:
		}
		select {
		case <-start:
			writer.WriteHeader(http.StatusNoContent)
		case <-request.Context().Done():
		}
	}))
	t.Cleanup(barrier.Close)

	const documentsPerVersion = 24
	peer := exec.CommandContext(t.Context(), peerBinary)
	peer.Env = append(os.Environ(),
		"OPENSEARCH_URL="+endpoint,
		"OPENSEARCH_MIXED_ALIAS="+alias,
		"OPENSEARCH_MIXED_PHYSICAL="+physical,
		"OPENSEARCH_MIXED_BARRIER="+barrier.URL,
		fmt.Sprintf("OPENSEARCH_MIXED_DOCUMENTS=%d", documentsPerVersion),
	)
	var peerOutput bytes.Buffer
	peer.Stdout, peer.Stderr = &peerOutput, &peerOutput
	if err := peer.Start(); err != nil {
		t.Fatal(err)
	}
	peerDone := make(chan error, 1)
	go func() { peerDone <- peer.Wait() }()
	select {
	case <-ready:
		close(start)
	case err := <-peerDone:
		t.Fatalf("mixed-application v1 peer exited before overlap: %v: %s", err, boundedPeerOutput(peerOutput.Bytes()))
	case <-time.After(10 * time.Second):
		t.Fatal("mixed-application v1 peer did not reach overlap barrier")
	}

	for position := range documentsPerVersion {
		id := fmt.Sprintf("application-v2-%03d", position)
		document, documentErr := search.NewDocument(tenant, logicalIndex, id, 1,
			json.RawMessage(fmt.Sprintf(`{"writer":"application-v2","sequence":%d}`, position)), limits)
		if documentErr != nil {
			t.Fatal(documentErr)
		}
		outcome, writeErr := current.Write(t.Context(), search.IndexDocument(document), search.RefreshWaitFor)
		if writeErr != nil || outcome.State != search.OutcomeApplied || outcome.Version != 1 {
			t.Fatalf("current mixed-application write %d = %#v/%v", position, outcome, writeErr)
		}
		if position%4 == 3 {
			result, searchErr := current.Search(t.Context(), search.Request{
				Tenant: tenant, Index: logicalIndex,
				Query: search.TermQuery{Field: "writer", Value: search.StringValue("application-v2")},
				Sort:  []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
				Page:  search.OffsetPage{Size: 64},
			})
			if searchErr != nil || len(result.Hits()) != position+1 {
				t.Fatalf("current mixed-application search %d = %d hits/%v", position, len(result.Hits()), searchErr)
			}
		}
	}
	select {
	case err := <-peerDone:
		if err != nil {
			t.Fatalf("mixed-application v1 peer failed: %v: %s", err, boundedPeerOutput(peerOutput.Bytes()))
		}
	case <-time.After(30 * time.Second):
		t.Fatal("mixed-application v1 peer exceeded its execution bound")
	}

	result, err := current.Search(t.Context(), search.Request{
		Tenant: tenant, Index: logicalIndex, Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page: search.OffsetPage{Size: documentsPerVersion * 2},
	})
	if err != nil || len(result.Hits()) != documentsPerVersion*2 {
		t.Fatalf("mixed-application final search = %d hits/%v", len(result.Hits()), err)
	}
	expectedIDs := make([]string, 0, documentsPerVersion*2)
	for position := range documentsPerVersion {
		expectedIDs = append(expectedIDs,
			fmt.Sprintf("application-v1-%03d", position),
			fmt.Sprintf("application-v2-%03d", position),
		)
	}
	sort.Strings(expectedIDs)
	for position, hit := range result.Hits() {
		if hit.Index != logicalIndex || hit.ID != expectedIDs[position] || hit.Version != 1 {
			t.Fatalf("mixed-application hit %d = %#v, want logical index %q ID %q version 1", position, hit, logicalIndex, expectedIDs[position])
		}
	}
}

func TestFrozenMixedApplicationPeerBuildsOutsideAdapterModule(t *testing.T) {
	t.Parallel()

	peerBinary := buildFrozenMixedApplicationPeer(t)
	info, err := os.Stat(peerBinary)
	if err != nil || info.IsDir() || info.Mode()&0o100 == 0 {
		t.Fatalf("frozen mixed-application peer = %#v/%v", info, err)
	}
}

func buildFrozenMixedApplicationPeer(t *testing.T) string {
	t.Helper()

	source, err := os.ReadFile(filepath.Join("testdata", "mixedappv1", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "main.go"), source, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(directory, "go.mod"),
		[]byte("module github.com/faustbrian/golib/internal/mixedappv1\n\ngo 1.26.6\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	peerBinary := filepath.Join(directory, "mixed-application-v1")
	build := exec.CommandContext(t.Context(), "go", "build", "-trimpath", "-o", peerBinary, ".")
	build.Dir = directory
	build.Env = standaloneGoBuildEnvironment(os.Environ())
	buildOutput, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("build frozen mixed-application v1 peer: %v: %s", err, boundedPeerOutput(buildOutput))
	}
	return peerBinary
}

func standaloneGoBuildEnvironment(environment []string) []string {
	result := make([]string, 0, len(environment)+2)
	for _, value := range environment {
		name, _, _ := strings.Cut(value, "=")
		if name != "GOFLAGS" && name != "GOWORK" {
			result = append(result, value)
		}
	}
	return append(result, "GOFLAGS=", "GOWORK=off")
}

func boundedPeerOutput(output []byte) string {
	const maximum = 4 << 10
	if len(output) > maximum {
		output = output[:maximum]
	}
	return string(output)
}
