package eventqueue

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func TestPublicErrorsRedactWrappedDiagnosticsForEveryCommonFormat(t *testing.T) {
	t.Parallel()

	const secret = "redis://queue-user:broker-password@queue.internal/0"
	tests := map[string]error{
		"envelope": envelopeFailure(
			ErrEnvelopeInvalid,
			secretDiagnostic{value: "payload metadata " + secret},
		),
		"dispatch": queueDispatchFailure(
			1,
			0,
			1,
			1,
			AcceptanceUnknown,
			secretDiagnostic{value: "backend timeout " + secret},
		),
		"handler": taskHandlingFailure(
			secretDiagnostic{value: "consumer panic " + secret},
		),
	}

	for name, diagnostic := range tests {
		diagnostic := diagnostic
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
				formatted := fmt.Sprintf(format, diagnostic)
				if strings.Contains(formatted, secret) {
					t.Fatalf("%s disclosed wrapped diagnostics: %s", format, formatted)
				}
				want := diagnostic.Error()
				if format == "%q" {
					want = strconv.Quote(want)
				}
				if formatted != want {
					t.Fatalf("%s formatted = %q, want %q", format, formatted, want)
				}
			}
		})
	}
}

type secretDiagnostic struct {
	value string
}

func (diagnostic secretDiagnostic) Error() string {
	return diagnostic.value
}
