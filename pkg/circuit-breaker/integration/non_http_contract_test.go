package integration_test

import (
	"context"
	"errors"
	"testing"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
)

var (
	errRPCUnavailable     = errors.New("rpc unavailable")
	errRPCInvalidArgument = errors.New("rpc invalid argument")
	errPostgresNoRows     = errors.New("postgres no rows")
	errPostgresBadConn    = errors.New("postgres bad connection")
	errValkeyMiss         = errors.New("valkey miss")
	errValkeyUnavailable  = errors.New("valkey unavailable")
	errStorageNotFound    = errors.New("storage not found")
	errStorageUnavailable = errors.New("storage unavailable")
)

func TestNonHTTPProtocolPoliciesRemainCallerOwned(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		operation  error
		classifier breaker.Classifier
		want       breaker.Outcome
	}{
		{
			name:      "rpc unavailable",
			operation: errRPCUnavailable,
			classifier: classifyErrors(
				[]error{errRPCUnavailable},
				[]error{errRPCInvalidArgument},
			),
			want: breaker.OutcomeFailure,
		},
		{
			name:      "rpc invalid argument",
			operation: errRPCInvalidArgument,
			classifier: classifyErrors(
				[]error{errRPCUnavailable},
				[]error{errRPCInvalidArgument},
			),
			want: breaker.OutcomeIgnored,
		},
		{
			name:      "postgres no rows",
			operation: errPostgresNoRows,
			classifier: classifyErrors(
				[]error{errPostgresBadConn},
				[]error{errPostgresNoRows},
			),
			want: breaker.OutcomeIgnored,
		},
		{
			name:      "postgres bad connection",
			operation: errPostgresBadConn,
			classifier: classifyErrors(
				[]error{errPostgresBadConn},
				[]error{errPostgresNoRows},
			),
			want: breaker.OutcomeFailure,
		},
		{
			name:      "valkey miss is healthy",
			operation: errValkeyMiss,
			classifier: func(completion breaker.Completion) breaker.Outcome {
				if errors.Is(completion.Err, errValkeyUnavailable) {
					return breaker.OutcomeFailure
				}
				return breaker.OutcomeSuccess
			},
			want: breaker.OutcomeSuccess,
		},
		{
			name:      "valkey unavailable",
			operation: errValkeyUnavailable,
			classifier: func(completion breaker.Completion) breaker.Outcome {
				if errors.Is(completion.Err, errValkeyUnavailable) {
					return breaker.OutcomeFailure
				}
				return breaker.OutcomeSuccess
			},
			want: breaker.OutcomeFailure,
		},
		{
			name:      "storage not found",
			operation: errStorageNotFound,
			classifier: classifyErrors(
				[]error{errStorageUnavailable},
				[]error{errStorageNotFound},
			),
			want: breaker.OutcomeIgnored,
		},
		{
			name:      "storage unavailable",
			operation: errStorageUnavailable,
			classifier: classifyErrors(
				[]error{errStorageUnavailable},
				[]error{errStorageNotFound},
			),
			want: breaker.OutcomeFailure,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			circuit := newFailureCircuit(t, test.classifier)
			_, err := breaker.Execute(context.Background(), circuit, func(context.Context) (struct{}, error) {
				return struct{}{}, test.operation
			})
			if err != test.operation {
				t.Fatalf("Execute() error = %v, want exact protocol error", err)
			}
			snapshot := circuit.Snapshot()
			switch test.want {
			case breaker.OutcomeSuccess:
				if snapshot.Successes != 1 || snapshot.State != breaker.StateClosed {
					t.Fatalf("success Snapshot() = %+v", snapshot)
				}
			case breaker.OutcomeFailure:
				if snapshot.Failures != 1 || snapshot.State != breaker.StateOpen {
					t.Fatalf("failure Snapshot() = %+v", snapshot)
				}
			case breaker.OutcomeIgnored:
				if snapshot.Ignored != 1 || snapshot.WindowClassified != 0 || snapshot.State != breaker.StateClosed {
					t.Fatalf("ignored Snapshot() = %+v", snapshot)
				}
			}
		})
	}
}

func classifyErrors(failures, ignored []error) breaker.Classifier {
	return func(completion breaker.Completion) breaker.Outcome {
		for _, failure := range failures {
			if errors.Is(completion.Err, failure) {
				return breaker.OutcomeFailure
			}
		}
		for _, ignore := range ignored {
			if errors.Is(completion.Err, ignore) {
				return breaker.OutcomeIgnored
			}
		}
		return breaker.OutcomeSuccess
	}
}
