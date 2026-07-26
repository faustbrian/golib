package httpclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestSpecBuildsLayeredIndependentTrailers(t *testing.T) {
	t.Parallel()

	body, _ := NewBytesBody("text/plain", []byte("payload"))
	spec := mustRequestSpec(t, "https://api.example.com/", "widgets")
	spec, _ = spec.WithBody(body)
	values := []string{"client"}
	var err error
	spec, err = spec.WithTrailer(LayerClient, "X-Checksum", values...)
	if err != nil {
		t.Fatalf("client trailer: %v", err)
	}
	values[0] = "mutated"
	spec, err = spec.WithTrailer(LayerRequest, "X-Checksum", "request")
	if err != nil {
		t.Fatalf("request trailer: %v", err)
	}
	spec, err = spec.AddTrailer(LayerRequest, "X-Checksum", "second")
	if err != nil {
		t.Fatalf("append trailer: %v", err)
	}
	spec, err = spec.WithTrailer(LayerClient, "X-Removed", "inherited")
	if err != nil {
		t.Fatalf("inherited trailer: %v", err)
	}
	spec, err = spec.WithoutTrailer(LayerRequest, "X-Removed")
	if err != nil {
		t.Fatalf("remove trailer: %v", err)
	}

	first, err := spec.Build(context.Background(), http.MethodPost)
	if err != nil {
		t.Fatalf("first build: %v", err)
	}
	second, err := spec.Build(context.Background(), http.MethodPost)
	if err != nil {
		t.Fatalf("second build: %v", err)
	}
	t.Cleanup(func() {
		_ = first.Body.Close()
		_ = second.Body.Close()
	})
	if got := first.Trailer.Values("X-Checksum"); len(got) != 2 || got[0] != "request" || got[1] != "second" || first.Trailer.Get("X-Removed") != "" {
		t.Fatalf("first trailers = %#v", first.Trailer)
	}
	first.Trailer.Set("X-Checksum", "changed")
	if got := second.Trailer.Values("X-Checksum"); len(got) != 2 || got[0] != "request" || got[1] != "second" {
		t.Fatalf("second trailers = %#v", second.Trailer)
	}
}

func TestRequestSpecTrailersReachStandardHTTPServer(t *testing.T) {
	t.Parallel()

	received := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		received <- request.Trailer.Clone()
		writer.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	body, _ := NewBytesBody("text/plain", []byte("payload"))
	spec := mustRequestSpec(t, server.URL+"/", "upload")
	spec, _ = spec.WithBody(body)
	spec, _ = spec.WithTrailer(LayerRequest, "Digest", "sha-256=:digest:")
	request, err := spec.Build(context.Background(), http.MethodPost)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	_ = response.Body.Close()
	if trailer := <-received; trailer.Get("Digest") != "sha-256=:digest:" {
		t.Fatalf("received trailers = %#v", trailer)
	}
}

func TestRequestSpecRejectsUnsafeOrBodylessTrailers(t *testing.T) {
	t.Parallel()

	spec := mustRequestSpec(t, "https://api.example.com/", "widgets")
	for _, name := range []string{"", "bad name", "Content-Length", "Authorization", "Trailer"} {
		if _, err := spec.WithTrailer(LayerRequest, name, "value"); !errors.Is(err, ErrInvalidTrailer) {
			t.Fatalf("trailer %q error = %v", name, err)
		}
	}
	if _, err := spec.WithTrailer(RequestLayer(255), "X-Trailer", "value"); !errors.Is(err, ErrInvalidRequestSpec) {
		t.Fatalf("invalid layer error = %v", err)
	}
	if _, err := spec.AddTrailer(RequestLayer(255), "X-Trailer", "value"); !errors.Is(err, ErrInvalidRequestSpec) {
		t.Fatalf("invalid add layer error = %v", err)
	}
	if _, err := spec.AddTrailer(LayerRequest, "bad name", "value"); !errors.Is(err, ErrInvalidTrailer) {
		t.Fatalf("invalid add name error = %v", err)
	}
	if _, err := spec.AddTrailer(LayerRequest, "X-Trailer"); !errors.Is(err, ErrInvalidTrailer) {
		t.Fatalf("empty add values error = %v", err)
	}
	if _, err := spec.WithTrailer(LayerRequest, "X-Trailer", "bad\nvalue"); !errors.Is(err, ErrInvalidTrailer) {
		t.Fatalf("invalid value error = %v", err)
	}
	if _, err := spec.WithoutTrailer(LayerRequest, "bad name"); !errors.Is(err, ErrInvalidTrailer) {
		t.Fatalf("invalid removal error = %v", err)
	}
	if _, err := spec.WithoutTrailer(RequestLayer(255), "X-Trailer"); !errors.Is(err, ErrInvalidRequestSpec) {
		t.Fatalf("invalid removal layer error = %v", err)
	}
	spec, _ = spec.WithTrailer(LayerRequest, "X-Trailer", "value")
	if _, err := spec.Build(context.Background(), http.MethodPost); !errors.Is(err, ErrInvalidTrailer) {
		t.Fatalf("bodyless trailer build error = %v", err)
	}

	body, _ := NewBytesBody("text/plain", []byte("payload"))
	spec, _ = spec.WithBody(body)
	spec, _ = spec.WithoutTrailer(LayerRequest, "X-Trailer")
	spec, err := spec.AddTrailer(LayerRequest, "X-Trailer", "restored")
	if err != nil {
		t.Fatalf("restore trailer: %v", err)
	}
	request, err := spec.Build(context.Background(), http.MethodPost)
	if err != nil {
		t.Fatalf("build restored trailer: %v", err)
	}
	defer func() { _ = request.Body.Close() }()
	if !strings.EqualFold(request.Trailer.Get("X-Trailer"), "restored") {
		t.Fatalf("restored trailer = %#v", request.Trailer)
	}
}
