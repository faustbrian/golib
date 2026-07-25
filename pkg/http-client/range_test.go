package httpclient

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestWithRangeClonesRequestAndAppliesStrongValidator(t *testing.T) {
	t.Parallel()

	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.test/artifact", nil)
	request.Header.Set("Range", "bytes=old")
	ranged, err := WithRange(request, RangeOptions{
		Offset: 128, Length: 64, Validator: RangeValidator{ETag: `"version-1"`},
	})
	if err != nil {
		t.Fatalf("construct range request: %v", err)
	}
	if ranged == request || ranged.Header.Get("Range") != "bytes=128-191" ||
		ranged.Header.Get("If-Range") != `"version-1"` || request.Header.Get("Range") != "bytes=old" {
		t.Fatalf("ranged request = %#v, original range %q", ranged, request.Header.Get("Range"))
	}

	modified := time.Unix(1_700_000_000, 0).UTC()
	ranged, err = WithRange(request, RangeOptions{
		Offset: 128, Validator: RangeValidator{LastModified: modified},
	})
	if err != nil || ranged.Header.Get("Range") != "bytes=128-" ||
		ranged.Header.Get("If-Range") != modified.Format(http.TimeFormat) {
		t.Fatalf("date range request = %#v, %v", ranged, err)
	}
}

func TestValidateRangeResponseContinuesRestartsOrCompletes(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name         string
		status       int
		contentRange string
		length       int64
		options      RangeResponseOptions
		want         RangeDisposition
		wantTotal    int64
	}{
		{
			name: "continue", status: http.StatusPartialContent,
			contentRange: "bytes 128-191/1024", length: 64,
			options: RangeResponseOptions{Offset: 128, Length: 64},
			want:    RangeContinue, wantTotal: 1024,
		},
		{
			name: "restart", status: http.StatusOK, length: 1024,
			options: RangeResponseOptions{Offset: 128, AllowRestart: true},
			want:    RangeRestart, wantTotal: 1024,
		},
		{
			name: "already complete", status: http.StatusRequestedRangeNotSatisfiable,
			contentRange: "bytes */128", length: 0,
			options: RangeResponseOptions{Offset: 128},
			want:    RangeComplete, wantTotal: 128,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := &http.Response{
				StatusCode: test.status, Header: make(http.Header),
				Body: http.NoBody, ContentLength: test.length,
			}
			if test.contentRange != "" {
				response.Header.Set("Content-Range", test.contentRange)
			}
			metadata, disposition, err := ValidateRangeResponse(response, test.options)
			if err != nil || disposition != test.want || metadata.Total != test.wantTotal {
				t.Fatalf("range result = %#v, %d, %v", metadata, disposition, err)
			}
		})
	}
}

func TestRangePolicyRejectsUnsafeRequestsAndMismatchedResponses(t *testing.T) {
	t.Parallel()

	request, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.test", strings.NewReader("body"))
	for _, options := range []RangeOptions{
		{Offset: -1},
		{Offset: 1, Length: -1},
		{Offset: 1, Validator: RangeValidator{ETag: `W/"weak"`}},
		{Offset: 1, Validator: RangeValidator{ETag: `"etag"`, LastModified: time.Now()}},
	} {
		if _, err := WithRange(request, options); !errors.Is(err, ErrInvalidRange) {
			t.Fatalf("invalid range options %#v error = %v", options, err)
		}
	}

	for _, test := range []struct {
		name          string
		status        int
		contentRange  string
		contentLength int64
		header        http.Header
		options       RangeResponseOptions
		want          error
	}{
		{
			name: "restart denied", status: http.StatusOK,
			options: RangeResponseOptions{Offset: 10}, want: ErrRangeRestartRequired,
		},
		{
			name: "start mismatch", status: http.StatusPartialContent,
			contentRange: "bytes 9-19/20", contentLength: 11,
			options: RangeResponseOptions{Offset: 10}, want: ErrRangeMismatch,
		},
		{
			name: "length mismatch", status: http.StatusPartialContent,
			contentRange: "bytes 10-19/20", contentLength: 9,
			options: RangeResponseOptions{Offset: 10}, want: ErrRangeMismatch,
		},
		{
			name: "etag mismatch", status: http.StatusPartialContent,
			contentRange: "bytes 10-19/20", contentLength: 10,
			header:  http.Header{"ETag": {`"other"`}},
			options: RangeResponseOptions{Offset: 10, Validator: RangeValidator{ETag: `"expected"`}},
			want:    ErrRangeValidatorMismatch,
		},
		{
			name: "malformed complete", status: http.StatusRequestedRangeNotSatisfiable,
			contentRange: "invalid", options: RangeResponseOptions{Offset: 10}, want: ErrRangeMismatch,
		},
		{
			name: "unexpected status", status: http.StatusBadGateway,
			options: RangeResponseOptions{Offset: 10}, want: ErrRangeMismatch,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			header := test.header.Clone()
			if header == nil {
				header = make(http.Header)
			}
			header.Set("Content-Range", test.contentRange)
			response := &http.Response{
				StatusCode: test.status, Header: header, Body: http.NoBody,
				ContentLength: test.contentLength,
			}
			_, _, err := ValidateRangeResponse(response, test.options)
			if !errors.Is(err, test.want) {
				t.Fatalf("range error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestRangePolicyParserAndValidatorBoundaries(t *testing.T) {
	t.Parallel()

	if (&RangeError{Operation: "test"}).Error() == "" {
		t.Fatal("range error rendered empty text")
	}
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.test", nil)
	if _, err := WithRange(request, RangeOptions{Offset: 1<<63 - 2, Length: 4}); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("overflow range error = %v", err)
	}
	if _, _, err := ValidateRangeResponse(nil, RangeResponseOptions{}); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("nil response error = %v", err)
	}
	if _, _, err := ValidateRangeResponse(&http.Response{
		StatusCode: http.StatusPartialContent, Header: make(http.Header), Body: http.NoBody,
	}, RangeResponseOptions{Offset: -1}); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("invalid response policy error = %v", err)
	}
	for _, etag := range []string{"weak", `"bad value"`, "\"bad\x7f\"", `"bad"quote"`} {
		if validStrongETag(etag) {
			t.Fatalf("invalid strong ETag accepted: %q", etag)
		}
	}
	modified := time.Unix(1_700_000_000, 123).UTC()
	header := http.Header{"Last-Modified": {modified.Truncate(time.Second).Format(http.TimeFormat)}}
	if !rangeValidatorMatches(header, RangeValidator{LastModified: modified}) ||
		rangeValidatorMatches(make(http.Header), RangeValidator{LastModified: modified}) {
		t.Fatal("Last-Modified validator comparison failed")
	}

	for _, test := range []struct {
		value       string
		unsatisfied bool
		want        bool
	}{
		{value: "bytes 0-1", want: false},
		{value: "bytes 0-1/invalid", want: false},
		{value: "bytes */*", unsatisfied: true, want: false},
		{value: "bytes invalid/10", want: false},
		{value: "bytes 2-1/10", want: false},
		{value: "bytes 0-10/10", want: false},
		{value: "bytes 0-1/*", want: true},
	} {
		_, ok := parseContentRange(test.value, test.unsatisfied)
		if ok != test.want {
			t.Fatalf("parse %q = %t, want %t", test.value, ok, test.want)
		}
	}
}
