package bodylimit_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/http-middleware/bodylimit"
)

func TestLimitCountsEncodedAndMultipartTransportBytes(t *testing.T) {
	t.Parallel()
	var compressed bytes.Buffer
	encoder := gzip.NewWriter(&compressed)
	_, _ = io.WriteString(encoder, strings.Repeat("compressible", 100))
	_ = encoder.Close()

	var multipartBody bytes.Buffer
	multipartWriter := multipart.NewWriter(&multipartBody)
	part, _ := multipartWriter.CreateFormField("payload")
	_, _ = io.WriteString(part, "value")
	_ = multipartWriter.Close()

	for _, tc := range []struct {
		name, contentType, encoding string
		payload                     []byte
		limit                       int64
		want                        int
	}{
		{name: "compressed exact", encoding: "gzip", payload: compressed.Bytes(), limit: int64(compressed.Len()), want: http.StatusNoContent},
		{name: "compressed overflow", encoding: "gzip", payload: compressed.Bytes(), limit: int64(compressed.Len() - 1), want: http.StatusRequestEntityTooLarge},
		{name: "multipart exact", contentType: multipartWriter.FormDataContentType(), payload: multipartBody.Bytes(), limit: int64(multipartBody.Len()), want: http.StatusNoContent},
		{name: "multipart overflow", contentType: multipartWriter.FormDataContentType(), payload: multipartBody.Bytes(), limit: int64(multipartBody.Len() - 1), want: http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			limit, err := bodylimit.New(bodylimit.Policy{MaxBytes: tc.limit})
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(tc.payload))
			request.Header.Set("Content-Type", tc.contentType)
			request.Header.Set("Content-Encoding", tc.encoding)
			recorder := httptest.NewRecorder()
			limit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, readErr := io.ReadAll(r.Body)
				if readErr != nil {
					var maximum *http.MaxBytesError
					if !errors.As(readErr, &maximum) {
						t.Errorf("read error = %v", readErr)
					}
					return
				}
				w.WriteHeader(http.StatusNoContent)
			})).ServeHTTP(recorder, request)
			if recorder.Code != tc.want {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.want)
			}
		})
	}
}

func TestLimitAppliesToUnreadBytesAndPreservesCancellation(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("prefix-body"))
	prefix := make([]byte, 7)
	if _, err := io.ReadFull(request.Body, prefix); err != nil {
		t.Fatal(err)
	}
	request.ContentLength = -1
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	request = request.WithContext(ctx)

	limit, _ := bodylimit.New(bodylimit.Policy{MaxBytes: 4})
	recorder := httptest.NewRecorder()
	limit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !errors.Is(r.Context().Err(), context.Canceled) {
			t.Fatalf("context error = %v", r.Context().Err())
		}
		payload, err := io.ReadAll(r.Body)
		if err != nil || string(payload) != "body" {
			t.Fatalf("payload = %q, error = %v", payload, err)
		}
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d", recorder.Code)
	}
}
