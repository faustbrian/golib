package localizedconfig_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/faustbrian/golib/pkg/config/decode"
	localized "github.com/faustbrian/golib/pkg/localized"
	"github.com/faustbrian/golib/pkg/localized/localizedconfig"
)

func TestConfigValueHookDecodesLocalizedMapTransactionally(t *testing.T) {
	t.Parallel()
	var target localizedconfig.Text
	if err := decode.Value(map[string]any{"EN-us": "Hello", "fi": "Hei"}, &target); err != nil {
		t.Fatalf("decode.Value() error = %v", err)
	}
	if !target.Valid {
		t.Fatal("Valid = false")
	}
	if got, ok := target.Localized.Get(mustLocale(t, "en-US")); !ok || got != "Hello" {
		t.Fatalf("Get(en-US) = %q, %v", got, ok)
	}

	before := target
	if err := decode.Value(map[string]any{"en": 42}, &target); err == nil {
		t.Fatal("decode.Value() error = nil")
	}
	if target.Valid != before.Valid || !target.Localized.Equal(before.Localized) {
		t.Fatal("failed decode mutated target")
	}
}

func TestConfigValueHookBoundaryFailures(t *testing.T) {
	t.Parallel()

	if got := localizedconfig.ErrInvalidValue.Error(); got != "localized config: invalid value" {
		t.Fatalf("Error() = %q", got)
	}
	var nilTarget *localizedconfig.Text
	if err := nilTarget.UnmarshalConfigValue(nil); !errors.Is(err, localizedconfig.ErrInvalidValue) {
		t.Fatalf("nil target error = %v", err)
	}
	var target localizedconfig.Text
	if err := target.UnmarshalConfigValue(map[string]string{"en": "Hello"}); err != nil || !target.Valid {
		t.Fatalf("string map = %+v, %v", target, err)
	}
	if err := target.UnmarshalConfigValue(42); !errors.Is(err, localizedconfig.ErrInvalidValue) {
		t.Fatalf("unsupported input error = %v", err)
	}
	if err := target.UnmarshalConfigValue(map[string]string{"en_": "bad"}); !errors.Is(err, localized.ErrInvalidLocale) {
		t.Fatalf("invalid locale error = %v", err)
	}
}

func TestConfigValueHookDistinguishesNull(t *testing.T) {
	t.Parallel()
	target := localizedconfig.NewText(func() localized.Text {
		value, _ := localized.TextFromMap(map[string]string{"en": "Hello"})
		return value
	}())
	if err := decode.Value(nil, &target); err != nil {
		t.Fatal(err)
	}
	if target.Valid || !target.Localized.IsEmpty() {
		t.Fatalf("target = %+v", target)
	}
}

func TestConfigTextTargetRequestsStringMap(t *testing.T) {
	t.Parallel()
	var target localizedconfig.Text
	if got, want := target.ConfigTextTarget(), reflect.TypeFor[map[string]string](); got != want {
		t.Fatalf("ConfigTextTarget() = %v, want %v", got, want)
	}
}
