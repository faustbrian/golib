package httpclient

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDefaultTransportDisablesImplicitCompression(t *testing.T) {
	t.Parallel()

	if !defaultTransport().DisableCompression {
		t.Fatal("default transport permits implicit decompression")
	}
}

func TestCompressionMiddlewareDecodesGzipWithExplicitMetadata(t *testing.T) {
	t.Parallel()

	middleware, err := NewCompressionMiddleware(CompressionOptions{
		Name: "gzip", Layer: MiddlewareClient,
		MaximumDecompressedBytes: 64,
		MaximumExpansionRatio:    20,
	})
	if err != nil {
		t.Fatalf("construct compression middleware: %v", err)
	}
	client, err := New(Config{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Header.Get("Accept-Encoding") != "gzip" {
				t.Fatalf("Accept-Encoding = %q", request.Header.Get("Accept-Encoding"))
			}
			compressed := gzipBytes(t, []byte("compressed response"))
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Encoding": {"gzip"},
					"Content-Length":   {"999"},
				},
				Body: io.NopCloser(bytes.NewReader(compressed)), Request: request,
				ContentLength: int64(len(compressed)),
			}, nil
		}),
		Middleware: []Middleware{middleware},
	})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.test", nil)
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("execute request: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read decoded body: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close decoded body: %v", err)
	}
	if string(body) != "compressed response" || !response.Uncompressed ||
		response.ContentLength != -1 || response.Header.Get("Content-Encoding") != "" ||
		response.Header.Get("Content-Length") != "" {
		t.Fatalf("decoded response = %#v, body %q", response, body)
	}
}

func TestCompressionMiddlewareEnforcesAbsoluteAndRatioBounds(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		maximum int64
		ratio   float64
		body    string
	}{
		{name: "absolute", maximum: 4, ratio: 100, body: "more than four"},
		{name: "ratio", maximum: 1 << 20, ratio: 1.1, body: strings.Repeat("a", 4<<10)},
	} {
		t.Run(test.name, func(t *testing.T) {
			middleware, err := NewCompressionMiddleware(CompressionOptions{
				Name: "bounded-gzip", Layer: MiddlewareClient,
				MaximumDecompressedBytes: test.maximum,
				MaximumExpansionRatio:    test.ratio,
			})
			if err != nil {
				t.Fatalf("construct compression middleware: %v", err)
			}
			compressed := gzipBytes(t, []byte(test.body))
			policy := middleware.around
			request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.test", nil)
			response, err := policy(request, func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Encoding": {"gzip"}},
					Body:       io.NopCloser(bytes.NewReader(compressed)), Request: request,
				}, nil
			})
			if err != nil {
				t.Fatalf("apply compression middleware: %v", err)
			}
			_, err = io.ReadAll(response.Body)
			if !errors.Is(err, ErrDecompressionLimit) {
				t.Fatalf("read error = %v", err)
			}
			_ = response.Body.Close()
		})
	}
}

func TestCompressionMiddlewareRejectsUnsupportedOrMalformedEncoding(t *testing.T) {
	t.Parallel()

	middleware, err := NewCompressionMiddleware(CompressionOptions{
		Name: "gzip", Layer: MiddlewareClient,
		MaximumDecompressedBytes: 64, MaximumExpansionRatio: 20,
	})
	if err != nil {
		t.Fatalf("construct compression middleware: %v", err)
	}
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.test", nil)
	for _, test := range []struct {
		name     string
		encoding string
		body     []byte
		want     error
	}{
		{name: "unsupported", encoding: "br", body: []byte("body"), want: ErrUnsupportedContentEncoding},
		{name: "malformed gzip", encoding: "gzip", body: []byte("not gzip"), want: ErrDecompression},
	} {
		t.Run(test.name, func(t *testing.T) {
			var closed bool
			_, err := middleware.around(request, func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Encoding": {test.encoding}},
					Body: &compressionCloseBody{
						Reader: bytes.NewReader(test.body), closed: &closed,
					},
					Request: request,
				}, nil
			})
			if !errors.Is(err, test.want) || !closed {
				t.Fatalf("compression error = %v, closed = %t", err, closed)
			}
		})
	}
}

func TestCompressionMiddlewareStreamsReplayableRequestGzip(t *testing.T) {
	t.Parallel()

	middleware, err := NewCompressionMiddleware(CompressionOptions{
		Name: "request-gzip", Layer: MiddlewareClient,
		CompressRequests: true, MinimumRequestBytes: 4,
	})
	if err != nil {
		t.Fatalf("construct compression middleware: %v", err)
	}
	content := []byte("request payload")
	request, _ := http.NewRequestWithContext(
		context.Background(), http.MethodPost, "https://example.test",
		io.NopCloser(bytes.NewReader(content)),
	)
	request.ContentLength = int64(len(content))
	request.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(content)), nil
	}
	response, err := middleware.around(request, func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Content-Encoding") != "gzip" || request.ContentLength != -1 ||
			request.Header.Get("Content-Length") != "" || request.GetBody == nil {
			t.Fatalf("compressed request metadata = %#v", request)
		}
		assertGzipRequestBody(t, request.Body, string(content))
		replay, replayErr := request.GetBody()
		if replayErr != nil {
			t.Fatalf("open compressed replay: %v", replayErr)
		}
		assertGzipRequestBody(t, replay, string(content))
		return &http.Response{
			StatusCode: http.StatusNoContent, Header: make(http.Header),
			Body: http.NoBody, Request: request,
		}, nil
	})
	if err != nil || response == nil {
		t.Fatalf("compressed request response = %#v, %v", response, err)
	}
	_ = response.Body.Close()
}

func TestCompressionMiddlewarePreservesOneShotAndExplicitEncodingSemantics(t *testing.T) {
	t.Parallel()

	middleware, err := NewCompressionMiddleware(CompressionOptions{
		Name: "request-gzip", Layer: MiddlewareClient, CompressRequests: true,
		MinimumRequestBytes: 8,
	})
	if err != nil {
		t.Fatalf("construct compression middleware: %v", err)
	}
	for _, test := range []struct {
		name           string
		content        string
		length         int64
		encoding       string
		wantCompressed bool
	}{
		{name: "one shot unknown length", content: "streamed request", length: -1, wantCompressed: true},
		{name: "below threshold", content: "small", length: 5},
		{name: "already encoded", content: "encoded request", length: 15, encoding: "br"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, _ := http.NewRequestWithContext(
				context.Background(), http.MethodPost, "https://example.test",
				io.NopCloser(strings.NewReader(test.content)),
			)
			request.ContentLength = test.length
			if test.encoding != "" {
				request.Header.Set("Content-Encoding", test.encoding)
			}
			_, err := middleware.around(request, func(request *http.Request) (*http.Response, error) {
				if test.wantCompressed {
					if request.GetBody != nil {
						t.Fatal("one-shot body became replayable")
					}
					assertGzipRequestBody(t, request.Body, test.content)
				} else {
					body, readErr := io.ReadAll(request.Body)
					if readErr != nil || string(body) != test.content ||
						request.Header.Get("Content-Encoding") != test.encoding {
						t.Fatalf("preserved request = %q, %v, encoding %q", body, readErr, request.Header.Get("Content-Encoding"))
					}
					_ = request.Body.Close()
				}
				return &http.Response{
					StatusCode: http.StatusNoContent, Header: make(http.Header),
					Body: http.NoBody, Request: request,
				}, nil
			})
			if err != nil {
				t.Fatalf("execute compression middleware: %v", err)
			}
		})
	}
}

func TestCompressionPolicyFailureAndLifecycleBoundaries(t *testing.T) {
	t.Parallel()

	if (&CompressionError{Operation: "test"}).Error() == "" ||
		(&DecompressionLimitError{}).Error() == "" {
		t.Fatal("compression errors rendered empty text")
	}
	requestFailure := errors.New("request")
	if !errors.Is(&CompressionError{Operation: "request compression", Cause: requestFailure}, ErrCompression) {
		t.Fatal("request compression error lacks category")
	}
	for _, options := range []CompressionOptions{
		{MinimumRequestBytes: -1},
		{MaximumDecompressedBytes: -1},
		{MaximumExpansionRatio: -1},
	} {
		if _, err := NewCompressionMiddleware(options); !errors.Is(err, ErrInvalidCompression) {
			t.Fatalf("invalid options error = %v", err)
		}
	}

	middleware, err := NewCompressionMiddleware(CompressionOptions{
		Name: "boundaries", Layer: MiddlewareClient, CompressRequests: true,
		MaximumDecompressedBytes: 4, MaximumExpansionRatio: 100,
	})
	if err != nil {
		t.Fatalf("construct compression middleware: %v", err)
	}
	request, _ := http.NewRequestWithContext(
		context.Background(), http.MethodPost, "https://example.test",
		io.NopCloser(strings.NewReader("request")),
	)
	request.ContentLength = 7
	request.GetBody = func() (io.ReadCloser, error) { return nil, requestFailure }
	_, err = middleware.around(request, func(request *http.Request) (*http.Response, error) {
		_, replayErr := request.GetBody()
		if !errors.Is(replayErr, requestFailure) {
			t.Fatalf("replay error = %v", replayErr)
		}
		_ = request.Body.Close()
		return nil, requestFailure
	})
	if !errors.Is(err, requestFailure) {
		t.Fatalf("next error = %v", err)
	}

	request, _ = http.NewRequestWithContext(
		context.Background(), http.MethodPost, "https://example.test",
		io.NopCloser(strings.NewReader("request")),
	)
	request.ContentLength = 7
	request.GetBody = func() (io.ReadCloser, error) { return nil, nil }
	_, err = middleware.around(request, func(request *http.Request) (*http.Response, error) {
		_, replayErr := request.GetBody()
		if !errors.Is(replayErr, ErrInvalidBody) {
			t.Fatalf("nil replay error = %v", replayErr)
		}
		_ = request.Body.Close()
		return &http.Response{
			StatusCode: http.StatusNoContent, Header: http.Header{"Content-Encoding": {"identity"}},
			Body: http.NoBody, Request: request,
		}, nil
	})
	if err != nil {
		t.Fatalf("identity response error = %v", err)
	}

	closeFailure := errors.New("close")
	compressed := gzipBytes(t, []byte("four"))
	response, err := middleware.around(request, func(request *http.Request) (*http.Response, error) {
		_ = request.Body.Close()
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Encoding": {"gzip"}},
			Body: &compressionErrorBody{
				Reader: bytes.NewReader(compressed), closeErr: closeFailure,
			},
			Request: request,
		}, nil
	})
	if err != nil {
		t.Fatalf("construct decoded response: %v", err)
	}
	if count, readErr := response.Body.Read(nil); count != 0 || readErr != nil {
		t.Fatalf("zero decompression read = %d, %v", count, readErr)
	}
	buffer := make([]byte, 4)
	if count, readErr := response.Body.Read(buffer); count != 4 || (readErr != nil && !errors.Is(readErr, io.EOF)) {
		t.Fatalf("exact decompression read = %d, %v", count, readErr)
	}
	if count, readErr := response.Body.Read(buffer); count != 0 || !errors.Is(readErr, io.EOF) {
		t.Fatalf("exact decompression EOF = %d, %v", count, readErr)
	}
	if closeErr := response.Body.Close(); !errors.Is(closeErr, closeFailure) {
		t.Fatalf("decoded close error = %v", closeErr)
	}
	if closeErr := response.Body.Close(); !errors.Is(closeErr, closeFailure) {
		t.Fatalf("second decoded close error = %v", closeErr)
	}

	limited := gzipBytes(t, []byte("too large"))
	response, err = middleware.around(request, func(request *http.Request) (*http.Response, error) {
		_ = request.Body.Close()
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Encoding": {"gzip"}},
			Body: io.NopCloser(bytes.NewReader(limited)), Request: request,
		}, nil
	})
	if err != nil {
		t.Fatalf("construct limited response: %v", err)
	}
	_, err = io.ReadAll(response.Body)
	if !errors.Is(err, ErrDecompressionLimit) {
		t.Fatalf("decompression limit error = %v", err)
	}
	if _, secondErr := response.Body.Read(buffer); !errors.Is(secondErr, ErrDecompressionLimit) {
		t.Fatalf("terminal decompression error = %v", secondErr)
	}
	_ = response.Body.Close()

	corrupted := gzipBytes(t, []byte("four"))
	corrupted[len(corrupted)-1] ^= 0xff
	response, err = middleware.around(request, func(request *http.Request) (*http.Response, error) {
		_ = request.Body.Close()
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Encoding": {"gzip"}},
			Body: io.NopCloser(bytes.NewReader(corrupted)), Request: request,
		}, nil
	})
	if err != nil {
		t.Fatalf("construct corrupt response: %v", err)
	}
	_, err = io.ReadAll(response.Body)
	if !errors.Is(err, ErrDecompression) {
		t.Fatalf("corrupt decompression error = %v", err)
	}
	_ = response.Body.Close()

	if wrapped := wrapCompressionCloseError(nil); wrapped != nil {
		t.Fatalf("nil close error = %v", wrapped)
	}
}

func TestCompressionRequestStreamPreservesSourceFailures(t *testing.T) {
	t.Parallel()

	sourceFailure := errors.New("source")
	body := newGzipRequestBody(&compressionErrorBody{
		Reader: &responseErrorReader{err: sourceFailure}, closeErr: sourceFailure,
	})
	_, err := io.ReadAll(body)
	if !errors.Is(err, ErrCompression) || !errors.Is(err, sourceFailure) {
		t.Fatalf("request stream error = %v", err)
	}
	if closeErr := body.Close(); !errors.Is(closeErr, sourceFailure) {
		t.Fatalf("request stream close error = %v", closeErr)
	}
	if closeErr := body.Close(); !errors.Is(closeErr, sourceFailure) {
		t.Fatalf("second request stream close error = %v", closeErr)
	}
}

func TestCompressionWorkerStopsWhenAttemptMiddlewareShortCircuits(t *testing.T) {
	t.Parallel()

	compression, err := NewCompressionMiddleware(CompressionOptions{
		Name: "request-gzip", Layer: MiddlewareClient, CompressRequests: true,
	})
	if err != nil {
		t.Fatalf("construct compression middleware: %v", err)
	}
	var compressed *gzipRequestBody
	shortCircuit, err := NewTransportMiddleware(MiddlewareOptions{
		Name: "short-circuit", Scope: ScopeAttempt, Layer: MiddlewareClient,
	}, func(request *http.Request, _ Next) (*http.Response, error) {
		var ok bool
		compressed, ok = request.Body.(*gzipRequestBody)
		if !ok {
			t.Fatalf("compressed body type = %T", request.Body)
		}

		return &http.Response{
			StatusCode: http.StatusNoContent, Header: make(http.Header),
			Body: http.NoBody, Request: request,
		}, nil
	})
	if err != nil {
		t.Fatalf("construct short-circuit middleware: %v", err)
	}
	pipeline, err := NewPipeline(compression, shortCircuit)
	if err != nil {
		t.Fatalf("construct pipeline: %v", err)
	}
	request, _ := http.NewRequestWithContext(
		context.Background(), http.MethodPost, "https://example.test",
		io.NopCloser(strings.NewReader("request payload")),
	)
	request.ContentLength = int64(len("request payload"))
	response, err := pipeline.Execute(request, roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("short-circuited request reached transport")
		return nil, nil
	}))
	if err != nil {
		t.Fatalf("execute pipeline: %v", err)
	}
	_ = response.Body.Close()
	select {
	case <-compressed.done:
	default:
		t.Fatal("compression worker remained active after attempt short circuit")
	}
}

func assertGzipRequestBody(t *testing.T, body io.ReadCloser, want string) {
	t.Helper()
	reader, err := gzip.NewReader(body)
	if err != nil {
		t.Fatalf("open request gzip: %v", err)
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read request gzip: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close request gzip: %v", err)
	}
	if err := body.Close(); err != nil {
		t.Fatalf("close request body: %v", err)
	}
	if string(decoded) != want {
		t.Fatalf("request body = %q, want %q", decoded, want)
	}
}

func gzipBytes(t *testing.T, content []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(content); err != nil {
		t.Fatalf("write gzip: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buffer.Bytes()
}

type compressionCloseBody struct {
	io.Reader
	closed *bool
}

type compressionErrorBody struct {
	io.Reader
	closeErr error
}

func (body *compressionErrorBody) Close() error { return body.closeErr }

func (body *compressionCloseBody) Close() error {
	*body.closed = true
	return nil
}
