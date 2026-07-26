package expression_test

import (
	"errors"
	"testing"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/expression"
)

func TestEvaluateServerUsesDefaultsAndValidatedOverrides(t *testing.T) {
	t.Parallel()

	host := "api.example.com"
	port := "443"
	hostVariable, err := openrpc.NewServerVariable(openrpc.ServerVariableInput{Default: &host})
	if err != nil {
		t.Fatal(err)
	}
	portVariable, err := openrpc.NewServerVariable(openrpc.ServerVariableInput{
		Default: &port, Enum: []string{"443", "8443"}, HasEnum: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := openrpc.NewServer(openrpc.ServerInput{
		URL: "https://${host}:${port}/rpc",
		Variables: map[string]openrpc.ServerVariable{
			"host": hostVariable,
			"port": portVariable,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := expression.EvaluateServer(server, nil, expression.DefaultPolicy()); err != nil || got != "https://api.example.com:443/rpc" {
		t.Fatalf("default URL = %q, error = %v", got, err)
	}
	if got, err := expression.EvaluateServer(server, map[string]string{"port": "8443"}, expression.DefaultPolicy()); err != nil || got != "https://api.example.com:8443/rpc" {
		t.Fatalf("override URL = %q, error = %v", got, err)
	}
}

func TestEvaluateServerRejectsUnknownInvalidAndMissingVariables(t *testing.T) {
	t.Parallel()

	port := "443"
	variable, err := openrpc.NewServerVariable(openrpc.ServerVariableInput{
		Default: &port, Enum: []string{"443"}, HasEnum: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := openrpc.NewServer(openrpc.ServerInput{
		URL: "https://${host}:${port}", Variables: map[string]openrpc.ServerVariable{"port": variable},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		overrides map[string]string
		want      error
	}{
		{overrides: map[string]string{"other": "value"}, want: expression.ErrUnknownVariable},
		{overrides: map[string]string{"port": "80"}, want: expression.ErrVariableEnum},
		{overrides: nil, want: expression.ErrMissingValue},
	} {
		if _, err := expression.EvaluateServer(server, test.overrides, expression.DefaultPolicy()); !errors.Is(err, test.want) {
			t.Errorf("overrides %#v error = %v", test.overrides, err)
		}
	}
}
