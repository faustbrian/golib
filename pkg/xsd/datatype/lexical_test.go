package datatype_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/xsd/datatype"
)

func TestValidateBuiltInLexicalSpaces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		lexical string
		valid   bool
	}{
		{name: "float", lexical: "-1.25E+3", valid: true},
		{name: "float", lexical: "INF", valid: true},
		{name: "gMonth", lexical: "--02", valid: true},
		{name: "double", lexical: "NaN", valid: true},
		{name: "double", lexical: "Infinity", valid: false},
		{name: "hexBinary", lexical: "0aFE", valid: true},
		{name: "hexBinary", lexical: "abc", valid: false},
		{name: "base64Binary", lexical: "Y Q==\n", valid: true},
		{name: "base64Binary", lexical: "YQ=", valid: false},
		{name: "base64Binary", lexical: "é", valid: false},
		{name: "language", lexical: "en-US", valid: true},
		{name: "language", lexical: "en_US", valid: false},
		{name: "NCName", lexical: "customer-id", valid: true},
		{name: "NCName", lexical: "ns:customer", valid: false},
		{name: "Name", lexical: "ns:customer", valid: true},
		{name: "NMTOKEN", lexical: "123:value", valid: true},
		{name: "NMTOKEN", lexical: "", valid: false},
		{name: "NMTOKENS", lexical: "ok bad!", valid: false},
		{name: "ENTITIES", lexical: "ok 1bad", valid: false},
		{name: "QName", lexical: "prefix:value", valid: true},
		{name: "QName", lexical: "too:many:colons", valid: false},
		{name: "QName", lexical: "prefix:", valid: false},
		{name: "string", lexical: string([]byte{0xff}), valid: false},
		{name: "ID", lexical: "1customer", valid: false},
		{name: "IDREFS", lexical: "first second", valid: true},
		{name: "IDREFS", lexical: "", valid: false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name+"/"+test.lexical, func(t *testing.T) {
			t.Parallel()
			err := datatype.ValidateBuiltInLexical(test.name, test.lexical)
			if test.valid && err != nil {
				t.Fatalf("ValidateBuiltInLexical() error = %v", err)
			}
			if !test.valid && !errors.Is(err, datatype.ErrInvalidLexical) {
				t.Fatalf("ValidateBuiltInLexical() error = %v, want ErrInvalidLexical", err)
			}
		})
	}
}

func TestValidateBuiltInLexicalRejectsUnknownType(t *testing.T) {
	t.Parallel()

	err := datatype.ValidateBuiltInLexical("unknown", "value")
	if !errors.Is(err, datatype.ErrUnknownType) {
		t.Fatalf("ValidateBuiltInLexical() error = %v, want ErrUnknownType", err)
	}
}

func TestValidateCalendarAndDurationLexicalSpaces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		lexical string
		valid   bool
	}{
		{name: "dateTime", lexical: "2000-02-29T24:00:00Z", valid: true},
		{name: "dateTime", lexical: "2001-02-29T12:00:00Z", valid: false},
		{name: "date", lexical: "-0001-12-31+14:00", valid: true},
		{name: "date", lexical: "0000-01-01", valid: false},
		{name: "time", lexical: "23:59:59.999-05:30", valid: true},
		{name: "time", lexical: "24:00:00.1", valid: false},
		{name: "time", lexical: "not-a-time", valid: false},
		{name: "gYearMonth", lexical: "2024-02Z", valid: true},
		{name: "gYear", lexical: "2024", valid: true},
		{name: "gYear", lexical: "2024+15:00", valid: false},
		{name: "gYear", lexical: "00000", valid: false},
		{name: "gMonthDay", lexical: "--02-29", valid: true},
		{name: "gMonthDay", lexical: "--02-30", valid: false},
		{name: "gMonthDay", lexical: "--00-01", valid: false},
		{name: "gDay", lexical: "---31", valid: true},
		{name: "gMonth", lexical: "--12--Z", valid: true},
		{name: "duration", lexical: "-P1Y2M3DT4H5M6.7S", valid: true},
		{name: "duration", lexical: "P", valid: false},
		{name: "duration", lexical: "P1YT", valid: false},
		{name: "duration", lexical: "not-a-duration", valid: false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name+"/"+test.lexical, func(t *testing.T) {
			t.Parallel()
			err := datatype.ValidateBuiltInLexical(test.name, test.lexical)
			if test.valid && err != nil {
				t.Fatalf("ValidateBuiltInLexical() error = %v", err)
			}
			if !test.valid && !errors.Is(err, datatype.ErrInvalidLexical) {
				t.Fatalf("ValidateBuiltInLexical() error = %v, want ErrInvalidLexical", err)
			}
		})
	}
}
