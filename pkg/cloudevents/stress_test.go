package cloudevents

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
)

func TestConcurrentEncodingAndDecodingIsIsolated(t *testing.T) {
	t.Parallel()

	data, err := NewJSONData([]byte(`{"value":true}`))
	if err != nil {
		t.Fatal(err)
	}
	event, err := NewEvent(Attributes{ID: "1", Source: "/source", Type: "example"}, data)
	if err != nil {
		t.Fatal(err)
	}
	want, err := EncodeJSON(event)
	if err != nil {
		t.Fatal(err)
	}

	const workers = 32
	const iterations = 100
	var wait sync.WaitGroup
	errorsChannel := make(chan error, workers)
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range iterations {
				encoded, encodeErr := EncodeJSON(event)
				if encodeErr != nil {
					errorsChannel <- encodeErr
					return
				}
				if !bytes.Equal(encoded, want) {
					errorsChannel <- errors.New("nondeterministic concurrent encoding")
					return
				}
				if _, decodeErr := DecodeJSON(encoded, DefaultLimits()); decodeErr != nil {
					errorsChannel <- decodeErr
					return
				}
			}
		}()
	}
	wait.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		t.Fatal(err)
	}
}

func TestCodecSoakRoundTripsStableBytes(t *testing.T) {
	t.Parallel()

	value := []byte(`{"specversion":"1.0","id":"1","source":"/source","type":"example","data":{"value":true}}`)
	for range 10_000 {
		event, err := DecodeJSON(value, DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		value, err = EncodeJSON(event)
		if err != nil {
			t.Fatal(err)
		}
	}
}

type failingReader struct{ err error }

func (reader failingReader) Read([]byte) (int, error) { return 0, reader.err }

func TestHTTPDecodePropagatesBodyFailureWithoutClosingCallerResource(t *testing.T) {
	t.Parallel()

	want := errors.New("injected read failure")
	header := http.Header{
		"Ce-Specversion": {"1.0"},
		"Ce-Id":          {"1"},
		"Ce-Source":      {"/source"},
		"Ce-Type":        {"example"},
	}
	if _, err := DecodeHTTP(context.Background(), header, failingReader{err: want}, DefaultLimits()); !errors.Is(err, want) {
		t.Fatalf("DecodeHTTP() error = %v, want injected failure", err)
	}
}
