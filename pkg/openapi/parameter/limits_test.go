package parameter_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/parameter"
)

func TestCodecEnforcesByteAndItemLimits(t *testing.T) {
	t.Parallel()

	options := parameter.Options{
		Version:  mustVersion(t, "3.2.0"),
		Location: parameter.Query,
		Style:    parameter.Form,
		Limits:   parameter.Limits{MaxBytes: 12, MaxItems: 1},
	}
	array := arrayValue(t, stringValue(t, "one"), stringValue(t, "two"))
	if _, err := parameter.Encode("value", array, options); !errors.Is(err, parameter.ErrLimitExceeded) {
		t.Fatalf("encode item limit error = %v", err)
	}
	if _, err := parameter.Encode("value", stringValue(t, "café"), options); !errors.Is(err, parameter.ErrLimitExceeded) {
		t.Fatalf("encode byte limit error = %v", err)
	}
	if _, err := parameter.Decode(
		"value", "value=too-long", parameter.Primitive, options,
	); !errors.Is(err, parameter.ErrLimitExceeded) {
		t.Fatalf("decode byte limit error = %v", err)
	}
	itemOptions := options
	itemOptions.Limits.MaxBytes = 100
	if _, err := parameter.Decode(
		"value", "value=one,two", parameter.Array, itemOptions,
	); !errors.Is(err, parameter.ErrLimitExceeded) {
		t.Fatalf("decode item limit error = %v", err)
	}
	options.Limits.MaxBytes = -1
	if _, err := parameter.Encode("value", stringValue(t, "one"), options); !errors.Is(err, parameter.ErrInvalidOptions) {
		t.Fatalf("negative limit error = %v", err)
	}
}

func TestDefaultLimitsArePositive(t *testing.T) {
	t.Parallel()

	limits := parameter.DefaultLimits()
	if limits.MaxBytes < 1 || limits.MaxItems < 1 {
		t.Fatalf("invalid default limits: %#v", limits)
	}
}

func TestCodecAcceptsExactByteAndItemLimits(t *testing.T) {
	t.Parallel()

	options := parameter.Options{
		Version:  mustVersion(t, "3.2.0"),
		Location: parameter.Query,
		Style:    parameter.Form,
		Limits:   parameter.Limits{MaxBytes: len("value=x"), MaxItems: 1},
	}
	value := stringValue(t, "x")
	encoded, err := parameter.Encode("value", value, options)
	if err != nil || encoded != "value=x" {
		t.Fatalf("exact Encode() = %q, %v", encoded, err)
	}
	decoded, err := parameter.Decode("value", encoded, parameter.Primitive, options)
	text, valid := decoded.Text()
	if err != nil || !valid || text != "x" {
		t.Fatalf("exact Decode() = %#v, %v", decoded, err)
	}

	array := arrayValue(t, value)
	if _, err = parameter.Encode("value", array, options); err != nil {
		t.Fatalf("exact item Encode() error = %v", err)
	}
	if _, err = parameter.Decode(
		"value", "value=x", parameter.Array, options,
	); err != nil {
		t.Fatalf("exact item Decode() error = %v", err)
	}
}

func TestDecodeRejectsItemAmplificationBeforeMalformedTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		raw      string
		shape    parameter.Shape
		location parameter.Location
		style    parameter.Style
		explode  bool
	}{
		{name: "repeated array", raw: "value=ok&value=%", shape: parameter.Array, location: parameter.Query, style: parameter.Form, explode: true},
		{name: "flat array", raw: "ok,%", shape: parameter.Array, location: parameter.Header, style: parameter.Simple},
		{name: "encoded array", raw: "value=ok%20%", shape: parameter.Array, location: parameter.Query, style: parameter.SpaceDelimited},
		{name: "exploded object", raw: "ok=x&bad=%", shape: parameter.Object, location: parameter.Query, style: parameter.Form, explode: true},
		{name: "deep object", raw: "value%5Bok%5D=x&value%5Bbad%5D=%", shape: parameter.Object, location: parameter.Query, style: parameter.DeepObject, explode: true},
		{name: "flat object", raw: "ok,x,bad,%", shape: parameter.Object, location: parameter.Header, style: parameter.Simple},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			options := parameter.Options{
				Version:  mustVersion(t, "3.2.0"),
				Location: test.location,
				Style:    test.style,
				Explode:  test.explode,
				Limits:   parameter.Limits{MaxBytes: 100, MaxItems: 1},
			}
			if _, err := parameter.Decode(
				"value", test.raw, test.shape, options,
			); !errors.Is(err, parameter.ErrLimitExceeded) {
				t.Fatalf("amplified decode error = %v, want item limit", err)
			}
		})
	}
}

func TestDecodeAcceptsMaximumIntegerItemLimit(t *testing.T) {
	t.Parallel()

	options := parameter.Options{
		Version:  mustVersion(t, "3.2.0"),
		Location: parameter.Header,
		Style:    parameter.Simple,
		Limits: parameter.Limits{
			MaxBytes: 100,
			MaxItems: int(^uint(0) >> 1),
		},
	}
	if _, err := parameter.Decode(
		"value", "key,value", parameter.Object, options,
	); err != nil {
		t.Fatalf("maximum integer limit Decode() error = %v", err)
	}
}
