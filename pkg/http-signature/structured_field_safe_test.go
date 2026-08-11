package httpsignature

import (
	"errors"
	"testing"
	"time"

	"github.com/dunglas/httpsfv"
)

func TestStructuredFieldDependencyPanicsBecomeParseErrors(t *testing.T) {
	t.Parallel()

	malformedItem := `%"00000000000000"0000`
	if _, err := ParseSignatureInputs([]string{"sig=" + malformedItem}); !errors.Is(err, ErrInvalidSignatureInput) {
		t.Fatalf("ParseSignatureInputs() error = %v, want ErrInvalidSignatureInput", err)
	}
	if _, err := ParseSignatures([]string{"sig=" + malformedItem}); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("ParseSignatures() error = %v, want ErrInvalidSignature", err)
	}
	if _, err := ParseDigestField("sha-256=" + malformedItem); !errors.Is(err, ErrInvalidDigestField) {
		t.Fatalf("ParseDigestField() error = %v, want ErrInvalidDigestField", err)
	}
	if _, err := ParseDigestPreferences([]string{"sha-256=" + malformedItem}); !errors.Is(err, ErrInvalidDigestPreferences) {
		t.Fatalf("ParseDigestPreferences() error = %v, want ErrInvalidDigestPreferences", err)
	}
	if _, err := strictStructuredField([]string{"a, " + malformedItem}, StructuredFieldList); err == nil {
		t.Fatal("strictStructuredField(list) error = nil")
	}
	if _, err := strictStructuredField([]string{"a;x=" + malformedItem}, StructuredFieldItem); err == nil {
		t.Fatal("strictStructuredField(item) error = nil")
	}
}

func TestRFC8941ParsersRejectLeadingHTAB(t *testing.T) {
	t.Parallel()

	if _, err := ParseSignatureInputs([]string{"\tsig=(\"@method\")"}); !errors.Is(err, ErrInvalidSignatureInput) {
		t.Fatalf("ParseSignatureInputs() error = %v, want ErrInvalidSignatureInput", err)
	}
	if _, err := ParseSignatures([]string{"\tsig=:AA==:"}); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("ParseSignatures() error = %v, want ErrInvalidSignature", err)
	}
	if _, err := ParseDigestField("\tsha-256=:AA==:"); !errors.Is(err, ErrInvalidDigestField) {
		t.Fatalf("ParseDigestField() error = %v, want ErrInvalidDigestField", err)
	}
	if _, err := ParseDigestPreferences([]string{"\tsha-256=1"}); !errors.Is(err, ErrInvalidDigestPreferences) {
		t.Fatalf("ParseDigestPreferences() error = %v, want ErrInvalidDigestPreferences", err)
	}
	if _, err := strictStructuredField([]string{"\ta=1"}, StructuredFieldDictionary); err == nil {
		t.Fatal("strictStructuredField(dictionary) error = nil")
	}
	if _, err := strictStructuredField([]string{"\ta"}, StructuredFieldList); err == nil {
		t.Fatal("strictStructuredField(list) error = nil")
	}
}

func TestRFC8941ParsersNormalizePermittedBinaryVariations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		values    []string
		fieldType StructuredFieldType
		want      string
	}{
		{"missing padding", []string{":aGVsbG8:"}, StructuredFieldItem, ":aGVsbG8=:"},
		{"non-zero pad bits", []string{":iZ==:"}, StructuredFieldItem, ":iQ==:"},
		{"dictionary lines", []string{"a=:aGVsbG8:", "b=:iZ==:"}, StructuredFieldDictionary, "a=:aGVsbG8=:, b=:iQ==:"},
		{"inner list", []string{"(:aGVsbG8: :iZ==:)"}, StructuredFieldList, "(:aGVsbG8=: :iQ==:)"},
		{"parameter", []string{"token;binary=:aGVsbG8:"}, StructuredFieldItem, "token;binary=:aGVsbG8=:"},
		{"token colons", []string{"token:aGVsbG8:still-token"}, StructuredFieldItem, "token:aGVsbG8:still-token"},
		{"string colons", []string{`"value :aGVsbG8: unchanged"`}, StructuredFieldItem, `"value :aGVsbG8: unchanged"`},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			actual, err := strictStructuredField(test.values, test.fieldType)
			if err != nil || actual != test.want {
				t.Fatalf("strictStructuredField() = %q, %v, want %q", actual, err, test.want)
			}
		})
	}
}

func TestRFC8941FieldLinesCombineBeforeParsing(t *testing.T) {
	t.Parallel()

	item, err := strictStructuredField([]string{`"0000`, `"`}, StructuredFieldItem)
	if err != nil || item != `"0000, "` {
		t.Fatalf("combined Item = %q, %v", item, err)
	}
	dictionary, err := strictStructuredField([]string{"a=1\t", "\tb=2"}, StructuredFieldDictionary)
	if err != nil || dictionary != "a=1, b=2" {
		t.Fatalf("combined Dictionary = %q, %v", dictionary, err)
	}
}

func TestRFC8941ParsersRejectInvalidBinaryAlphabet(t *testing.T) {
	t.Parallel()

	for _, value := range []string{":aGVs\nbG8:", ":aGVs\rbG8:", ":aGVs-bG8:", ":aGVs_bG8:", ":a=GVsbG8:"} {
		if actual, err := strictStructuredField([]string{value}, StructuredFieldItem); err == nil {
			t.Fatalf("strictStructuredField(%q) = %q, nil, want failure", value, actual)
		}
	}
}

func TestRFC8941IntegralDecimalRepairBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name         string
		value        string
		decimalPoint int
		want         bool
	}{
		{"first byte", ".", 0, false},
		{"not after digit", "x.", 1, false},
		{"fraction present", "1.x", 1, false},
		{"negative integral", "-1.", 2, true},
		{"positive integral", "1.", 1, true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if actual := isEmptyRFC8941DecimalFraction(test.value, test.decimalPoint); actual != test.want {
				t.Fatalf("isEmptyRFC8941DecimalFraction(%q, %d) = %t, want %t", test.value, test.decimalPoint, actual, test.want)
			}
		})
	}
}

func TestRFC8941SerializersKeepIntegralDecimalFraction(t *testing.T) {
	t.Parallel()

	const field = `sig=("@method";token=v1.;text="value \" 1.";decimal=1.0)`
	inputs, err := ParseSignatureInputs([]string{field})
	if err != nil {
		t.Fatalf("ParseSignatureInputs() error = %v", err)
	}
	if actual := inputs.String(); actual != field {
		t.Fatalf("SignatureInputs.String() = %q, want %q", actual, field)
	}

	parameters := httpsfv.NewParams()
	parameters.Add("decimal", float64(1))
	innerList := httpsfv.InnerList{
		Items:  []httpsfv.Item{httpsfv.NewItem("@method")},
		Params: parameters,
	}
	actual, err := marshalRFC8941(innerList)
	if err != nil || actual != `("@method");decimal=1.0` {
		t.Fatalf("inner-list serialization = %q, %v", actual, err)
	}
}

func TestRFC8941SerializerRejectsUnsupportedAndOutOfRangeValues(t *testing.T) {
	t.Parallel()

	if _, err := marshalRFC8941(httpsfv.NewItem(time.Unix(1, 0))); !errors.Is(err, errStructuredFieldRFC8941) {
		t.Fatalf("date serialization error = %v, want errStructuredFieldRFC8941", err)
	}
	if _, err := marshalRFC8941(httpsfv.NewItem(float64(1e15))); err == nil {
		t.Fatal("out-of-range decimal serialization succeeded")
	}
}
