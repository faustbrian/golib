package integration_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
)

var (
	errLimited         = errors.New("locally rate limited")
	errBulkheadFull    = errors.New("local bulkhead full")
	errValidation      = errors.New("local validation")
	errTransport       = errors.New("transport unavailable")
	errFallbackApplied = errors.New("fallback applied")
)

type trackedBody struct {
	reader *strings.Reader
	reads  int
	closes int
}

func newTrackedBody(value string) *trackedBody {
	return &trackedBody{reader: strings.NewReader(value)}
}

func (b *trackedBody) Read(buffer []byte) (int, error) {
	b.reads++
	return b.reader.Read(buffer)
}

func (b *trackedBody) Close() error {
	b.closes++
	return nil
}

type httpContract struct {
	circuit   *breaker.Breaker
	stages    []string
	responses []*http.Response
	cacheHit  bool
	invalid   bool
	limited   bool
	bulkhead  bool
	terminal  error
	transport int
}

func (c *httpContract) execute(ctx context.Context) (*http.Response, error) {
	c.stages = append(c.stages, "cache")
	if c.cacheHit {
		return &http.Response{StatusCode: http.StatusOK, Body: newTrackedBody("cached")}, nil
	}
	c.stages = append(c.stages, "validation")
	if c.invalid {
		return nil, errValidation
	}
	c.stages = append(c.stages, "identity", "limiter")
	if c.limited {
		return nil, errLimited
	}
	c.stages = append(c.stages, "bulkhead")
	if c.bulkhead {
		return nil, errBulkheadFull
	}

	c.stages = append(c.stages, "operation-telemetry-start")
	response, err := breaker.Execute(ctx, c.circuit, func(context.Context) (*http.Response, error) {
		for attempt, response := range c.responses {
			c.stages = append(c.stages,
				"retry",
				"authentication",
				"signing",
				"attempt-telemetry",
				"transport",
			)
			c.transport++
			if response.StatusCode >= http.StatusInternalServerError && attempt+1 < len(c.responses) {
				if closeErr := response.Body.Close(); closeErr != nil {
					return nil, closeErr
				}
				continue
			}
			return response, nil
		}
		if c.terminal != nil {
			return nil, c.terminal
		}
		return nil, errTransport
	})
	c.stages = append(c.stages, "operation-telemetry-complete")
	return response, err
}

func TestHTTPCompositionRecordsOneLogicalOutcomeAcrossRetries(t *testing.T) {
	classifier := func(completion breaker.Completion) breaker.Outcome {
		if completion.Err != nil {
			return breaker.OutcomeFailure
		}
		response, ok := completion.Result.(*http.Response)
		if ok && response.StatusCode >= http.StatusInternalServerError {
			return breaker.OutcomeFailure
		}
		return breaker.OutcomeSuccess
	}
	circuit := newFailureCircuit(t, classifier)
	firstBody := newTrackedBody("retryable")
	finalBody := newTrackedBody("success")
	contract := &httpContract{
		circuit: circuit,
		responses: []*http.Response{
			{StatusCode: http.StatusServiceUnavailable, Body: firstBody},
			{StatusCode: http.StatusOK, Body: finalBody},
		},
	}

	response, err := contract.execute(context.Background())
	if err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if response.StatusCode != http.StatusOK || contract.transport != 2 {
		t.Fatalf("response/transport = %d/%d", response.StatusCode, contract.transport)
	}
	if firstBody.closes != 1 || firstBody.reads != 0 {
		t.Fatalf("retry body closes/reads = %d/%d, want 1/0", firstBody.closes, firstBody.reads)
	}
	if finalBody.closes != 0 || finalBody.reads != 0 {
		t.Fatalf("caller body changed before ownership transfer: closes/reads = %d/%d", finalBody.closes, finalBody.reads)
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatalf("read final body: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close final body: %v", err)
	}

	wantStages := []string{
		"cache", "validation", "identity", "limiter", "bulkhead", "operation-telemetry-start",
		"retry", "authentication", "signing", "attempt-telemetry", "transport",
		"retry", "authentication", "signing", "attempt-telemetry", "transport",
		"operation-telemetry-complete",
	}
	if strings.Join(contract.stages, ",") != strings.Join(wantStages, ",") {
		t.Fatalf("stages = %v, want %v", contract.stages, wantStages)
	}
	if got := circuit.Snapshot(); got.Admitted != 1 || got.Successes != 1 || got.Failures != 0 {
		t.Fatalf("logical-operation Snapshot() = %+v", got)
	}
}

func TestHTTPLocalShortCircuitsBypassBreakerAdmission(t *testing.T) {
	for _, test := range []struct {
		name     string
		contract httpContract
		wantErr  error
	}{
		{name: "cache hit", contract: httpContract{cacheHit: true}},
		{name: "validation", contract: httpContract{invalid: true}, wantErr: errValidation},
		{name: "limiter", contract: httpContract{limited: true}, wantErr: errLimited},
		{name: "bulkhead", contract: httpContract{bulkhead: true}, wantErr: errBulkheadFull},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			circuit := newFailureCircuit(t, nil)
			contract := test.contract
			contract.circuit = circuit
			response, err := contract.execute(context.Background())
			if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("execute() error = %v, want %v", err, test.wantErr)
			}
			if test.wantErr == nil {
				if err != nil || response == nil {
					t.Fatalf("cache execute() = %#v, %v", response, err)
				}
				_ = response.Body.Close()
			}
			if got := circuit.Snapshot(); got.Admitted != 0 || got.WindowClassified != 0 {
				t.Fatalf("short-circuit Snapshot() = %+v", got)
			}
		})
	}
}

func TestHTTPContextOutcomesRespectAdmissionBoundary(t *testing.T) {
	t.Run("canceled before admission", func(t *testing.T) {
		circuit := newFailureCircuit(t, nil)
		contract := &httpContract{circuit: circuit}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		response, err := contract.execute(ctx)
		if response != nil || !errors.Is(err, context.Canceled) || contract.transport != 0 {
			t.Fatalf("execute() = %#v, %v, transport %d", response, err, contract.transport)
		}
		if got := circuit.Snapshot(); got.Admitted != 0 || got.Completed != 0 {
			t.Fatalf("pre-admission cancellation Snapshot() = %+v", got)
		}
	})

	for _, terminal := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(terminal.Error(), func(t *testing.T) {
			circuit := newFailureCircuit(t, nil)
			contract := &httpContract{circuit: circuit, terminal: terminal}
			response, err := contract.execute(context.Background())
			if response != nil || !errors.Is(err, terminal) {
				t.Fatalf("execute() = %#v, %v, want %v", response, err, terminal)
			}
			if got := circuit.Snapshot(); got.Admitted != 1 || got.Completed != 1 ||
				got.TotalFailures != 1 || got.Failures != 1 {
				t.Fatalf("post-admission context outcome Snapshot() = %+v", got)
			}
		})
	}
}

func TestHTTPRejectionLeavesFallbackOutsideBreaker(t *testing.T) {
	circuit := newFailureCircuit(t, nil)
	_, _ = breaker.Execute(context.Background(), circuit, func(context.Context) (struct{}, error) {
		return struct{}{}, errTransport
	})
	contract := &httpContract{
		circuit:   circuit,
		responses: []*http.Response{{StatusCode: http.StatusOK, Body: newTrackedBody("unexpected")}},
	}
	response, err := contract.execute(context.Background())
	if !errors.Is(err, breaker.ErrOpen) || response != nil || contract.transport != 0 {
		t.Fatalf("open execute() = %#v, %v, transport %d", response, err, contract.transport)
	}
	fallbackCalled := false
	fallback := func(error) error {
		fallbackCalled = true
		return errFallbackApplied
	}
	fallbackErr := fallback(err)
	if !fallbackCalled {
		t.Fatal("caller fallback was not applied")
	}
	if !errors.Is(fallbackErr, errFallbackApplied) {
		t.Fatalf("fallback error = %v", fallbackErr)
	}
	if got := circuit.Snapshot(); got.Admitted != 1 || got.Rejected != 1 {
		t.Fatalf("fallback Snapshot() = %+v", got)
	}
}
