package media_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/faustbrian/golib/pkg/openapi/media"
)

type cancelingReader struct {
	cancel context.CancelFunc
	read   bool
}

func (reader *cancelingReader) Read(buffer []byte) (int, error) {
	if reader.read {
		return 0, io.EOF
	}
	reader.read = true
	buffer[0] = 'x'
	reader.cancel()
	return 1, nil
}

func TestParseServerSentEventsFollowsWHATWGFieldSemantics(t *testing.T) {
	t.Parallel()

	stream := "\ufeff: comment\r" +
		"data: first\r\ndata:second\n" +
		"event: update\rid: 7\nretry: 001500\nunknown: ignored\r\n\r\n" +
		"data: next\nid: bad\x00\nretry: nope\n\n" +
		": comment only\n\n" +
		"data: incomplete"
	items, err := media.ParseServerSentEvents(
		context.Background(), strings.NewReader(stream),
		media.DefaultServerSentEventLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d", len(items))
	}
	want := []string{
		`{"data":"first\nsecond","event":"update","id":"7","retry":1500}`,
		`{"data":"next","id":"7"}`,
	}
	for index, item := range items {
		encoded, marshalErr := item.MarshalJSON()
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if string(encoded) != want[index] {
			t.Fatalf("item %d = %s, want %s", index, encoded, want[index])
		}
	}
}

func TestParseServerSentEventsPreservesTypedFieldBoundaries(t *testing.T) {
	t.Parallel()

	stream := "data\nID: ignored\nid: first\n\n" +
		"data: \xff\nevent:first\nevent:\nid\nretry: 000\n\n" +
		"data: final\nretry\n\n"
	items, err := media.ParseServerSentEvents(
		context.Background(), strings.NewReader(stream),
		media.DefaultServerSentEventLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`{"data":"","id":"first"}`,
		`{"data":"�","id":"","retry":0}`,
		`{"data":"final","id":""}`,
	}
	if len(items) != len(want) {
		t.Fatalf("items = %d", len(items))
	}
	for index, item := range items {
		encoded, marshalErr := item.MarshalJSON()
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if string(encoded) != want[index] {
			t.Fatalf("item %d = %s, want %s", index, encoded, want[index])
		}
	}
}

func TestParseServerSentEventsEnforcesIndependentLimits(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		input  string
		limits media.ServerSentEventLimits
	}{
		{name: "bytes", input: "data: x\n\n",
			limits: media.ServerSentEventLimits{
				MaxBytes: 1, MaxLineBytes: 20, MaxDataBytes: 20, MaxEvents: 2,
			}},
		{name: "line", input: "data: long\n\n",
			limits: media.ServerSentEventLimits{
				MaxBytes: 20, MaxLineBytes: 2, MaxDataBytes: 20, MaxEvents: 2,
			}},
		{name: "data", input: "data: long\n\n",
			limits: media.ServerSentEventLimits{
				MaxBytes: 20, MaxLineBytes: 20, MaxDataBytes: 2, MaxEvents: 2,
			}},
		{name: "events", input: "data: one\n\ndata: two\n\n",
			limits: media.ServerSentEventLimits{
				MaxBytes: 40, MaxLineBytes: 20, MaxDataBytes: 20, MaxEvents: 1,
			}},
		{name: "crlf bytes", input: "\r\n",
			limits: media.ServerSentEventLimits{
				MaxBytes: 1, MaxLineBytes: 20, MaxDataBytes: 20, MaxEvents: 1,
			}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := media.ParseServerSentEvents(
				context.Background(), strings.NewReader(test.input), test.limits,
			)
			if !errors.Is(err, media.ErrServerSentEventLimit) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestParseServerSentEventsValidatesInputsAndReaderFailures(t *testing.T) {
	t.Parallel()

	limits := media.DefaultServerSentEventLimits()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, test := range []struct {
		name   string
		ctx    context.Context
		reader io.Reader
		limits media.ServerSentEventLimits
	}{
		{name: "nil context", reader: strings.NewReader(""), limits: limits},
		{name: "nil reader", ctx: context.Background(), limits: limits},
		{name: "invalid limits", ctx: context.Background(),
			reader: strings.NewReader(""), limits: media.ServerSentEventLimits{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := media.ParseServerSentEvents(test.ctx, test.reader, test.limits)
			if !errors.Is(err, media.ErrInvalidServerSentEvents) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	if _, err := media.ParseServerSentEvents(
		canceled, strings.NewReader(""), limits,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled error = %v", err)
	}
	readFailure := errors.New("reader failed")
	if _, err := media.ParseServerSentEvents(
		context.Background(), iotest.ErrReader(readFailure), limits,
	); !errors.Is(err, readFailure) ||
		!errors.Is(err, media.ErrInvalidServerSentEvents) {
		t.Fatalf("reader error = %v", err)
	}
	midstream, cancelMidstream := context.WithCancel(context.Background())
	if _, err := media.ParseServerSentEvents(
		midstream, &cancelingReader{cancel: cancelMidstream}, limits,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("midstream cancellation error = %v", err)
	}

	_, invalidErr := media.ParseServerSentEvents(
		context.Background(), strings.NewReader(""), media.ServerSentEventLimits{},
	)
	parseError, ok := invalidErr.(*media.ServerSentEventError)
	if !ok || !strings.Contains(parseError.Error(), "invalid_limits") {
		t.Fatalf("parse error = %#v", invalidErr)
	}
	var nilError *media.ServerSentEventError
	if nilError.Error() != "parse server-sent events: <nil>" ||
		nilError.Unwrap() != nil {
		t.Fatalf("nil error methods returned unexpected values")
	}
}

func TestParseServerSentEventsAcceptsEveryExactBoundary(t *testing.T) {
	t.Parallel()

	minimum := media.ServerSentEventLimits{
		MaxBytes: 1, MaxLineBytes: 1, MaxDataBytes: 1, MaxEvents: 1,
	}
	if items, err := media.ParseServerSentEvents(
		context.Background(), strings.NewReader(""), minimum,
	); err != nil || len(items) != 0 {
		t.Fatalf("minimum limits = %#v, %v", items, err)
	}
	if items, err := media.ParseServerSentEvents(
		context.Background(), strings.NewReader("x"), minimum,
	); err != nil || len(items) != 0 {
		t.Fatalf("exact byte and line limits = %#v, %v", items, err)
	}
	if _, err := media.ParseServerSentEvents(
		context.Background(), strings.NewReader("\r\n"),
		media.ServerSentEventLimits{
			MaxBytes: 2, MaxLineBytes: 1, MaxDataBytes: 1, MaxEvents: 1,
		},
	); err != nil {
		t.Fatalf("exact CRLF byte limit = %v", err)
	}
	stream := "data: x\nretry: 9\n\n"
	items, err := media.ParseServerSentEvents(
		context.Background(), strings.NewReader(stream),
		media.ServerSentEventLimits{
			MaxBytes: int64(len(stream)), MaxLineBytes: 8, MaxDataBytes: 2, MaxEvents: 1,
		},
	)
	if err != nil || len(items) != 1 {
		t.Fatalf("exact event limits = %#v, %v", items, err)
	}
	encoded, err := items[0].MarshalJSON()
	if err != nil || string(encoded) != `{"data":"x","retry":9}` {
		t.Fatalf("boundary retry event = %s, %v", encoded, err)
	}
}

func TestParseServerSentEventsRejectsCumulativeDataOverflow(t *testing.T) {
	t.Parallel()

	stream := "data: aa\ndata: aa\n\n"
	_, err := media.ParseServerSentEvents(
		context.Background(), strings.NewReader(stream),
		media.ServerSentEventLimits{
			MaxBytes: int64(len(stream)), MaxLineBytes: 8, MaxDataBytes: 5, MaxEvents: 1,
		},
	)
	if !errors.Is(err, media.ErrServerSentEventLimit) {
		t.Fatalf("cumulative data error = %v", err)
	}
}
