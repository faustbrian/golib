package reference

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const maximumFuzzResolverBody = 65_536

func FuzzFileResolverIdentifierBoundary(f *testing.F) {
	root := f.TempDir()
	path := filepath.Join(root, "schema.json")
	if err := os.WriteFile(path, []byte(`{"type":"string"}`), 0o600); err != nil {
		f.Fatal(err)
	}
	valid := (&url.URL{Scheme: "file", Path: path}).String()
	for _, seed := range []string{
		valid,
		"file:///etc/passwd",
		"file://server.example/schema.json",
		"file:///tmp/schema.json?token=secret",
		"%zz",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, identifier string) {
		first, firstErr := resolveFuzzFile(t, root, identifier)
		second, secondErr := resolveFuzzFile(t, root, identifier)
		assertStableResolution(t, first, firstErr, second, secondErr)
	})
}

func resolveFuzzFile(t *testing.T, root, identifier string) (Resource, error) {
	t.Helper()
	options := DefaultFileResolverOptions()
	options.AllowedRoots = []string{root}
	options.MaxBytes = maximumFuzzResolverBody
	options.MaxDocuments = 1
	resolver, err := NewFileResolver(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := resolver.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	return resolver.Resolve(context.Background(), identifier)
}

func FuzzHTTPResolverResponseBoundary(f *testing.F) {
	for _, seed := range []struct {
		body        []byte
		contentType string
		encoding    string
		status      uint16
	}{
		{body: []byte(`{"type":"string"}`), contentType: "application/json", status: 200},
		{body: []byte("type: integer\n"), contentType: "application/yaml", status: 200},
		{body: []byte(`{]`), contentType: "application/json", status: 200},
		{body: []byte(`{}`), contentType: "application/json", encoding: "gzip", status: 200},
		{body: []byte(`{}`), contentType: "text/plain", status: 404},
	} {
		f.Add(seed.body, seed.contentType, seed.encoding, seed.status)
	}
	f.Fuzz(func(
		t *testing.T,
		body []byte,
		contentType string,
		encoding string,
		status uint16,
	) {
		body = boundedFuzzBytes(body, maximumFuzzResolverBody)
		contentType = boundedFuzzText(contentType, 1_024)
		encoding = boundedFuzzText(encoding, 1_024)
		first, firstErr := resolveFuzzHTTP(t, body, contentType, encoding, status)
		second, secondErr := resolveFuzzHTTP(t, body, contentType, encoding, status)
		assertStableResolution(t, first, firstErr, second, secondErr)
	})
}

func resolveFuzzHTTP(
	t *testing.T,
	body []byte,
	contentType string,
	encoding string,
	status uint16,
) (Resource, error) {
	t.Helper()
	options := DefaultHTTPResolverOptions()
	options.AllowedHosts = []string{"example.test"}
	options.MaxBytes = maximumFuzzResolverBody
	options.MaxDocuments = 1
	options.Timeout = time.Second
	policy, err := newHTTPPolicy(options)
	if err != nil {
		t.Fatal(err)
	}
	resolver := &HTTPResolver{
		policy:       policy,
		semaphore:    make(chan struct{}, 1),
		maxBytes:     options.MaxBytes,
		maxDocuments: options.MaxDocuments,
		maxRedirects: options.MaxRedirects,
		timeout:      options.Timeout,
		parseLimits:  policy.parseLimits,
		createRequest: func(
			ctx context.Context,
			identifier string,
		) (*http.Request, error) {
			return http.NewRequestWithContext(
				ctx, http.MethodGet, identifier, nil,
			)
		},
	}
	resolver.client = &http.Client{Transport: fuzzRoundTripper(func(
		request *http.Request,
	) (*http.Response, error) {
		header := make(http.Header)
		header.Set("Content-Type", contentType)
		header.Set("Content-Encoding", encoding)
		return &http.Response{
			StatusCode:    int(status),
			Header:        header,
			Body:          io.NopCloser(bytes.NewReader(body)),
			ContentLength: int64(len(body)),
			Request:       request,
		}, nil
	})}
	return resolver.Resolve(
		context.Background(), "https://example.test/resource.json",
	)
}

type fuzzRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip fuzzRoundTripper) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	return roundTrip(request)
}

func boundedFuzzBytes(value []byte, maximum int) []byte {
	if len(value) > maximum {
		return value[:maximum]
	}
	return value
}

func boundedFuzzText(value string, maximum int) string {
	if len(value) > maximum {
		return value[:maximum]
	}
	return value
}

func assertStableResolution(
	t *testing.T,
	first Resource,
	firstErr error,
	second Resource,
	secondErr error,
) {
	t.Helper()
	if (firstErr == nil) != (secondErr == nil) {
		t.Fatalf("nondeterministic resolver errors: %v and %v", firstErr, secondErr)
	}
	if firstErr != nil {
		if firstErr.Error() != secondErr.Error() {
			t.Fatalf("nondeterministic resolver errors: %v and %v", firstErr, secondErr)
		}
		return
	}
	firstJSON, firstMarshalErr := first.Root.MarshalJSON()
	secondJSON, secondMarshalErr := second.Root.MarshalJSON()
	if firstMarshalErr != nil || secondMarshalErr != nil ||
		!bytes.Equal(firstJSON, secondJSON) ||
		first.RetrievalURI != second.RetrievalURI ||
		first.CanonicalURI != second.CanonicalURI {
		t.Fatalf("nondeterministic resolver resources")
	}
}
