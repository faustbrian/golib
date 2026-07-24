package config_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"strings"
	"testing"

	config "github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/decode"
	"github.com/faustbrian/golib/pkg/config/defaults"
	"github.com/faustbrian/golib/pkg/config/environment"
	tomlsource "github.com/faustbrian/golib/pkg/config/toml"
	"github.com/faustbrian/golib/pkg/config/validation"
	yamlsource "github.com/faustbrian/golib/pkg/config/yaml"
)

const diagnosticCanary = "canary-secret-diagnostic-value"

func TestSecretAndErrorsNeverLeakAcrossDiagnosticSurfaces(t *testing.T) {
	t.Parallel()

	cause := canaryDiagnosticError{}
	field := &decode.FieldError{
		Path: "value", Expected: "integer", Received: "string", Cause: cause,
	}
	diagnostics := map[string]any{
		"secret": config.NewSecret(diagnosticCanary),
		"secret container": struct {
			Token config.Secret
		}{Token: config.NewSecret(diagnosticCanary)},
		"source error":     &config.SourceError{Name: "source", Cause: cause},
		"decode field":     field,
		"decode aggregate": &decode.Errors{Fields: []*decode.FieldError{field}},
		"default error":    &defaults.Error{Path: "value", Expected: "int", Cause: cause},
		"environment error": &environment.MappingError{
			Path: "value", Name: "VALUE", Expected: "int", Received: "string", Cause: cause,
		},
		"YAML error":       &yamlsource.ParseError{Line: 1, Column: 1, Cause: cause},
		"TOML error":       &tomlsource.ParseError{Cause: cause},
		"validation error": validation.At("value", cause),
		"decode panic":     decodePanicError(t),
		"validation panic": validationPanicError(t),
	}

	for name, diagnostic := range diagnostics {
		name := name
		diagnostic := diagnostic
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for surface, output := range renderDiagnosticSurfaces(t, diagnostic) {
				if strings.Contains(output, diagnosticCanary) {
					t.Fatalf("%s leaked canary: %q", surface, output)
				}
			}
		})
	}
}

func renderDiagnosticSurfaces(t *testing.T, diagnostic any) map[string]string {
	t.Helper()

	outputs := map[string]string{
		"sprint":    fmt.Sprint(diagnostic),
		"string":    fmt.Sprintf("%s", diagnostic),
		"quoted":    fmt.Sprintf("%q", diagnostic),
		"value":     fmt.Sprintf("%v", diagnostic),
		"detailed":  fmt.Sprintf("%+v", diagnostic),
		"Go syntax": fmt.Sprintf("%#v", diagnostic),
	}
	encoded, err := json.Marshal(diagnostic)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	outputs["JSON"] = string(encoded)

	var standard bytes.Buffer
	log.New(&standard, "", 0).Printf("diagnostic=%v", diagnostic)
	outputs["standard log"] = standard.String()

	var structured bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&structured, nil))
	logger.Error("configuration diagnostic", slog.Any("diagnostic", diagnostic))
	outputs["structured log"] = structured.String()
	return outputs
}

func decodePanicError(t *testing.T) error {
	t.Helper()
	var destination panickingDiagnosticText
	err := decode.Value(diagnosticCanary, &destination)
	if err == nil {
		t.Fatal("decode.Value() panic error = nil")
	}
	return err
}

func validationPanicError(t *testing.T) error {
	t.Helper()
	err := validation.Run(
		context.Background(),
		struct{}{},
		func(context.Context, struct{}) error { panic(diagnosticCanary) },
	)
	if err == nil {
		t.Fatal("validation.Run() panic error = nil")
	}
	return err
}

type canaryDiagnosticError struct{}

func (canaryDiagnosticError) Error() string { return diagnosticCanary }

func (canaryDiagnosticError) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte(diagnosticCanary))
}

func (canaryDiagnosticError) MarshalJSON() ([]byte, error) {
	return json.Marshal(diagnosticCanary)
}

type panickingDiagnosticText string

func (*panickingDiagnosticText) UnmarshalText([]byte) error {
	panic(diagnosticCanary)
}
