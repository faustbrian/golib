package keyphrase_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	keyphrase "github.com/faustbrian/golib/pkg/keyphrase"
	"github.com/faustbrian/golib/pkg/keyphrase/bip39"
	"github.com/faustbrian/golib/pkg/keyphrase/passphrase"
	"github.com/faustbrian/golib/pkg/keyphrase/password"
)

type diagnosticCause struct {
	Message string
}

func (cause *diagnosticCause) Error() string {
	return cause.Message
}

func (cause *diagnosticCause) GoString() string {
	return cause.Message
}

func TestStructuredLoggingRedactsSecrets(t *testing.T) {
	t.Parallel()

	mnemonic, err := bip39.FromEntropy(make([]byte, 16), bip39.English)
	if err != nil {
		t.Fatalf("FromEntropy() error = %v", err)
	}
	secret := keyphrase.Secret("sensitive-value")

	for _, handler := range []func(*bytes.Buffer) slog.Handler{
		func(buffer *bytes.Buffer) slog.Handler { return slog.NewTextHandler(buffer, nil) },
		func(buffer *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(buffer, nil) },
	} {
		var output bytes.Buffer
		slog.New(handler(&output)).Info("generated", "secret", secret, "mnemonic", mnemonic)
		encoded := output.String()
		if strings.Contains(encoded, "sensitive") || strings.Contains(encoded, "abandon") {
			t.Fatalf("structured logging disclosed secret material")
		}
		if strings.Count(encoded, "redacted") != 2 {
			t.Fatalf("structured logging did not redact both values: %q", encoded)
		}
	}
}

func TestParsedPassphraseRepresentationsAreRedacted(t *testing.T) {
	t.Parallel()

	parsed := passphrase.Parsed{
		Prefix: keyphrase.Secret("prefix-secret"),
		Words:  []string{"classified", "phrase"},
		Suffix: keyphrase.Secret("suffix-secret"),
	}
	formatted := fmt.Sprintf("%+v", parsed)
	if strings.Contains(formatted, "secret") || strings.Contains(formatted, "classified") {
		t.Fatalf("formatting disclosed parsed passphrase: %q", formatted)
	}
	if !strings.Contains(formatted, "redacted") {
		t.Fatalf("formatting did not mark parsed passphrase redacted: %q", formatted)
	}

	for _, handler := range []func(*bytes.Buffer) slog.Handler{
		func(buffer *bytes.Buffer) slog.Handler { return slog.NewTextHandler(buffer, nil) },
		func(buffer *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(buffer, nil) },
	} {
		var output bytes.Buffer
		slog.New(handler(&output)).Info("parsed", "passphrase", parsed)
		encoded := output.String()
		if strings.Contains(encoded, "secret") || strings.Contains(encoded, "classified") {
			t.Fatalf("structured logging disclosed parsed passphrase: %q", encoded)
		}
		if !strings.Contains(encoded, "redacted") {
			t.Fatalf("structured logging did not mark parsed passphrase redacted: %q", encoded)
		}
	}
}

func TestTypedErrorFormattingDoesNotDiscloseWrappedCauses(t *testing.T) {
	t.Parallel()

	cause := &diagnosticCause{Message: "classified-source-diagnostic"}
	errors := []error{
		&keyphrase.Error{Code: keyphrase.CodeSource, Cause: cause},
		&password.Error{Code: password.CodeRandomness, Cause: cause},
		&passphrase.Error{Code: passphrase.CodeRandomness, Cause: cause},
		&bip39.Error{Code: bip39.CodeRandomness, Cause: cause},
	}
	for _, err := range errors {
		representations := []string{
			fmt.Sprintf("%v", err),
			fmt.Sprintf("%+v", err),
			fmt.Sprintf("%#v", err),
			fmt.Sprintf("%s", err),
			fmt.Sprintf("%q", err),
		}
		for _, representation := range representations {
			if strings.Contains(representation, "classified") {
				t.Fatalf("error formatting disclosed wrapped cause: %q", representation)
			}
		}
	}
}

func TestStandardJSONEncodingRedactsSensitiveValues(t *testing.T) {
	t.Parallel()

	mnemonic, err := bip39.FromEntropy(make([]byte, 16), bip39.English)
	if err != nil {
		t.Fatalf("FromEntropy() error = %v", err)
	}
	values := []any{
		keyphrase.Secret("sensitive-value"),
		mnemonic,
		passphrase.Parsed{
			Prefix: keyphrase.Secret("prefix-secret"),
			Words:  []string{"classified-word", "private-word"},
			Suffix: keyphrase.Secret("suffix-secret"),
		},
	}
	for _, value := range values {
		encoded, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			t.Fatalf("json.Marshal(%T) error = %v", value, marshalErr)
		}
		if !bytes.Contains(encoded, []byte("redacted")) {
			t.Fatalf("json.Marshal(%T) was not redacted: %s", value, encoded)
		}
	}

	cause := &diagnosticCause{Message: "classified-source-diagnostic"}
	for _, typedError := range []error{
		&keyphrase.Error{Code: keyphrase.CodeSource, Cause: cause},
		&password.Error{Code: password.CodeRandomness, Cause: cause},
		&passphrase.Error{Code: passphrase.CodeRandomness, Cause: cause},
		&bip39.Error{Code: bip39.CodeRandomness, Cause: cause},
	} {
		encoded, marshalErr := json.Marshal(typedError)
		if marshalErr != nil {
			t.Fatalf("json.Marshal(%T) error = %v", typedError, marshalErr)
		}
		if bytes.Contains(encoded, []byte("classified")) {
			t.Fatalf("json.Marshal(%T) disclosed wrapped cause: %s", typedError, encoded)
		}
	}
}

func TestRecoveredPanicsAndGenericDiagnosticsRedactSecrets(t *testing.T) {
	t.Parallel()

	secret := keyphrase.Secret("panic-secret")
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		panic(secret)
	}()

	for _, representation := range []string{
		fmt.Sprint(recovered),
		fmt.Sprintf("%+v", recovered),
		fmt.Sprintf("trace.attribute=%v", secret),
		fmt.Sprintf("metric.label=%v", secret),
		fmt.Sprintf("test diagnostic: %#v", secret),
	} {
		if strings.Contains(representation, "panic-secret") {
			t.Fatalf("generic diagnostic disclosed recovered secret: %q", representation)
		}
		if !strings.Contains(representation, "redacted") {
			t.Fatalf("generic diagnostic was not explicitly redacted: %q", representation)
		}
	}
}
