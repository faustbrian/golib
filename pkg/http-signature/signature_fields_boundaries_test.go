package httpsignature

import (
	"errors"
	"testing"
	"time"

	"github.com/dunglas/httpsfv"
)

func TestComponentNameCharacterBoundaries(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"a", "z", "0", "9", "!", "~", "@a", "@z", "@0", "@9", "@-"} {
		if !validComponentName(name) {
			t.Fatalf("validComponentName(%q) = false", name)
		}
	}
	for _, name := range []string{"", "@", "A", "@A", "@_", "@`", "@{", "@aA", "@a_"} {
		if validComponentName(name) {
			t.Fatalf("validComponentName(%q) = true", name)
		}
	}
	for _, character := range []byte{'a', 'z', '0', '9', '!', '~'} {
		if !isLowerFieldNameCharacter(character) {
			t.Fatalf("isLowerFieldNameCharacter(%q) = false", character)
		}
	}
	for _, character := range []byte{0, '/', ':', '@', '[', '{', 'A'} {
		if isLowerFieldNameCharacter(character) {
			t.Fatalf("isLowerFieldNameCharacter(%q) = true", character)
		}
	}
	for _, value := range []string{"", string([]byte{0x20}), string([]byte{0x7e})} {
		if !validSFStringValue(value) {
			t.Fatalf("validSFStringValue(%q) = false", value)
		}
	}
	for _, value := range []string{string([]byte{0x1f}), string([]byte{0x7f})} {
		if validSFStringValue(value) {
			t.Fatalf("validSFStringValue(%q) = true", value)
		}
	}
}

func TestStructuredFieldOWSNormalizationBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"\tkey=1", "\tkey=1"},
		{" \tkey=1", " \tkey=1"},
		{"key=1\t ", "key=1\t "},
		{"key=1\t, next=2", "key=1 , next=2"},
		{"key=1\t  , next=2", "key=1   , next=2"},
		{"a\tb", "a\tb"},
	}
	for _, test := range tests {
		if got := normalizeStructuredFieldOWS([]string{test.input}); len(got) != 1 || got[0] != test.want {
			t.Fatalf("normalizeStructuredFieldOWS(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestSignatureFieldParserResourceAndTypeBoundaries(t *testing.T) {
	t.Parallel()

	limits := DefaultSyntaxLimits()
	invalid := limits
	invalid.MaxFieldBytes = 0
	if _, err := ParseSignatureInputsWithLimits([]string{`sig=("@method")`}, invalid); !errors.Is(err, ErrInvalidSyntaxLimits) {
		t.Fatalf("input invalid limits error = %v", err)
	}
	if _, err := ParseSignaturesWithLimits([]string{"sig=:AA==:"}, invalid); !errors.Is(err, ErrInvalidSyntaxLimits) {
		t.Fatalf("signature invalid limits error = %v", err)
	}
	small := limits
	small.MaxFieldBytes = 2
	small.MaxBinaryBytes = 1
	if _, err := ParseSignatureInputsWithLimits([]string{`sig=("@method")`}, small); !errors.Is(err, ErrSyntaxLimit) {
		t.Fatalf("input field limit error = %v", err)
	}
	if _, err := ParseSignaturesWithLimits([]string{"sig=:AA==:"}, small); !errors.Is(err, ErrSyntaxLimit) {
		t.Fatalf("signature field limit error = %v", err)
	}
	members := limits
	members.MaxDictionaryMembers = 1
	if _, err := ParseSignatureInputsWithLimits([]string{`a=("@method"), b=("@method")`}, members); !errors.Is(err, ErrSyntaxLimit) {
		t.Fatalf("input member limit error = %v", err)
	}
	if _, err := ParseSignaturesWithLimits([]string{"a=:AA==:, b=:AA==:"}, members); !errors.Is(err, ErrSyntaxLimit) {
		t.Fatalf("signature member limit error = %v", err)
	}
	components := limits
	components.MaxComponentsPerSignature = 1
	if _, err := ParseSignatureInputsWithLimits([]string{`sig=("@method" "@path")`}, components); !errors.Is(err, ErrSyntaxLimit) {
		t.Fatalf("component limit error = %v", err)
	}
	parameters := limits
	parameters.MaxParametersPerItem = 1
	if _, err := ParseSignatureInputsWithLimits([]string{`sig=("@method");created=1;expires=2`}, parameters); !errors.Is(err, ErrSyntaxLimit) {
		t.Fatalf("signature parameter limit error = %v", err)
	}
	if _, err := ParseSignatureInputsWithLimits([]string{`sig=("@query-param";name="x";key="y")`}, parameters); !errors.Is(err, ErrSyntaxLimit) {
		t.Fatalf("component parameter limit error = %v", err)
	}
	binary := limits
	binary.MaxBinaryBytes = 1
	if _, err := ParseSignaturesWithLimits([]string{"sig=:AAE=:"}, binary); !errors.Is(err, ErrSyntaxLimit) {
		t.Fatalf("signature binary limit error = %v", err)
	}
	if _, err := ParseSignaturesWithLimits([]string{"sig=:AA==:;x;y"}, parameters); !errors.Is(err, ErrSyntaxLimit) {
		t.Fatalf("signature parameter limit error = %v", err)
	}

	for _, value := range []string{`sig="value"`, `sig=(1)`, `sig=("@")`, `sig=("@method";sf=?0)`, `sig=("@query-param";name=1)`, `sig=("@method" "@method")`} {
		if _, err := ParseSignatureInputs([]string{value}); !errors.Is(err, ErrInvalidSignatureInput) {
			t.Fatalf("ParseSignatureInputs(%q) error = %v", value, err)
		}
	}
	for _, value := range []string{`sig=("@method")`, `sig="value"`, "Bad", "sig=:A:", `sig="unterminated`, "sig=:AA==:;date=@1"} {
		if _, err := ParseSignatures([]string{value}); !errors.Is(err, ErrInvalidSignature) {
			t.Fatalf("ParseSignatures(%q) error = %v", value, err)
		}
	}
	if _, err := ParseSignatureInputs([]string{`sig=("@method";date=@1)`}); !errors.Is(err, ErrInvalidSignatureInput) {
		t.Fatalf("RFC 9651 component date error = %v", err)
	}
}

func TestStructuredFieldConversionAndValidationBoundaries(t *testing.T) {
	t.Parallel()

	if _, err := copyParameters(nil); err == nil {
		t.Fatal("copyParameters(nil) succeeded")
	}
	for _, value := range []any{true, "text", int64(1), 1.5, []byte("x"), httpsfv.Token("token")} {
		converted, ok := fromStructuredValue(value)
		if !ok || converted == nil {
			t.Fatalf("fromStructuredValue(%T) = %#v, %t", value, converted, ok)
		}
	}
	if _, ok := fromStructuredValue(struct{}{}); ok {
		t.Fatal("unsupported structured value converted")
	}
	unsupported := httpsfv.NewParams()
	unsupported.Add("date", time.Unix(1, 0))
	if _, err := copyParameters(unsupported); err == nil {
		t.Fatal("unsupported parameter value copied")
	}
	params := httpsfv.NewParams()
	addParameters(params, []Parameter{{Name: "token", Value: SFToken("value")}, {Name: "binary", Value: []byte("x")}})
	if len(params.Names()) != 2 {
		t.Fatalf("parameter names = %#v", params.Names())
	}

	for _, parameters := range [][]Parameter{
		{{Name: "created", Value: true}},
		{{Name: "nonce", Value: true}},
	} {
		if validSignatureParameters(parameters) {
			t.Fatalf("validSignatureParameters(%#v) = true", parameters)
		}
	}
	for _, parameters := range [][]Parameter{
		{{Name: "created", Value: false}},
		{{Name: "nonce", Value: true}},
	} {
		if validAcceptSignatureParameters(parameters) {
			t.Fatalf("validAcceptSignatureParameters(%#v) = true", parameters)
		}
	}
	for _, parameters := range [][]Parameter{
		{{Name: "sf", Value: false}},
		{{Name: "key", Value: true}},
	} {
		if validComponentParameters(parameters) {
			t.Fatalf("validComponentParameters(%#v) = true", parameters)
		}
	}
	for _, name := range []string{"", "@", "@Upper", "Upper"} {
		if validComponentName(name) {
			t.Fatalf("validComponentName(%q) = true", name)
		}
	}
	binary := []byte("x")
	clone := cloneParameterValue(binary).([]byte)
	clone[0] = 'y'
	if string(binary) != "x" || cloneParameterValue("text") != "text" {
		t.Fatal("cloneParameterValue did not preserve value ownership")
	}
}

func TestDictionarySplittingAndOWSBoundaries(t *testing.T) {
	t.Parallel()

	members, err := splitDictionaryMembers(`a=("comma,inside"), b="escaped\\\"quote"`)
	if err != nil || len(members) != 2 {
		t.Fatalf("splitDictionaryMembers() = %#v, %v", members, err)
	}
	for _, value := range []string{")", `a="unterminated`, `a="escaped\`} {
		if _, err := splitDictionaryMembers(value); err == nil {
			t.Fatalf("splitDictionaryMembers(%q) succeeded", value)
		}
	}
	if key := dictionaryMemberKey("\t key=:AA==:"); key != "key" {
		t.Fatalf("dictionaryMemberKey() = %q", key)
	}
	if key := dictionaryMemberKey("  !bad"); key != "" {
		t.Fatalf("invalid dictionaryMemberKey() = %q", key)
	}
	normalized := normalizeStructuredFieldOWS([]string{"\tkey=:AA==:\t,\tother=:AA==:\t", `key="tab\tinside"`})
	if normalized[0] != "\tkey=:AA==: , other=:AA==:\t" || normalized[1] != `key="tab\tinside"` {
		t.Fatalf("normalizeStructuredFieldOWS() = %#v", normalized)
	}
	spaced := normalizeStructuredFieldOWS([]string{" \t \tkey=:AA==:,\t \tother=:AA==:"})
	if spaced[0] != " \t \tkey=:AA==:,   other=:AA==:" {
		t.Fatalf("spaced OWS = %q", spaced[0])
	}
}

func TestSignatureFieldStringsPanicForImpossibleInvalidInternalLabels(t *testing.T) {
	t.Run("input", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("SignatureInputs.String() did not panic")
			}
		}()
		_ = (SignatureInputs{entries: []SignatureInput{{Label: "UPPER", Components: []ComponentIdentifier{{Name: "@method"}}}}}).String()
	})
	t.Run("signature", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("Signatures.String() did not panic")
			}
		}()
		_ = (Signatures{entries: []SignatureValue{{Label: "UPPER", Value: []byte("x")}}}).String()
	})
}
