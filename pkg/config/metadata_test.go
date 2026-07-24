package config_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	config "github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/configtest"
)

func TestLoadPropagatesSecretAndDeprecatedFieldMetadataToOrigins(t *testing.T) {
	t.Parallel()

	type credentials struct {
		Token string `config:"token"`
	}
	type settings struct {
		Legacy      string       `config:"legacy,deprecated"`
		Credentials *credentials `config:"credentials,secret"`
		Implicit    string
		Ignored     string `config:"-"`
		private     string
	}
	source := configtest.NewSource(
		config.SourceInfo{Name: "document"},
		config.Document{Tree: map[string]any{
			"legacy": "value",
			"credentials": map[string]any{
				"token": "canary-secret-value",
			},
			"implicit": "value",
		}},
	)
	plan := configtest.MustPlan(t, source)
	snapshot := configtest.MustLoad[settings](t, context.Background(), plan)

	legacy, ok := snapshot.Origin("legacy")
	if !ok || !legacy.Deprecated || legacy.Sensitive {
		t.Fatalf("legacy origin = %#v, %v", legacy, ok)
	}
	for _, path := range []string{"credentials", "credentials.token"} {
		origin, ok := snapshot.Origin(path)
		if !ok || !origin.Sensitive || origin.Deprecated {
			t.Fatalf("Origin(%q) = %#v, %v", path, origin, ok)
		}
	}
	implicit, ok := snapshot.Origin("implicit")
	if !ok || implicit.Sensitive || implicit.Deprecated {
		t.Fatalf("implicit origin = %#v, %v", implicit, ok)
	}
	value := snapshot.Value()
	if value.Ignored != "" || value.private != "" {
		t.Fatalf("ignored/private fields were populated: %#v", value)
	}
}

func TestSensitiveSourceMarksEveryNestedOriginWithoutRetainingValues(t *testing.T) {
	t.Parallel()

	type settings struct {
		Credentials struct {
			Token string `config:"token"`
		} `config:"credentials"`
	}
	source := configtest.NewSource(
		config.SourceInfo{Name: "sensitive-document", Sensitive: true},
		config.Document{Tree: map[string]any{
			"credentials": map[string]any{"token": "canary-secret-value"},
		}},
	)
	plan := configtest.MustPlan(t, source)
	snapshot := configtest.MustLoad[settings](t, context.Background(), plan)
	for _, path := range []string{"credentials", "credentials.token"} {
		origin, ok := snapshot.Origin(path)
		if !ok || !origin.Sensitive || origin.Source != "sensitive-document" {
			t.Fatalf("Origin(%q) = %#v, %v", path, origin, ok)
		}
		formatted := fmt.Sprintf("%#v", origin)
		if strings.Contains(formatted, "canary-secret-value") {
			t.Fatalf("Origin(%q) leaked source value: %q", path, formatted)
		}
	}
}
