package tabular

import (
	"errors"
	"reflect"
	"testing"
)

func TestNormalizeRowAppliesOnlyConfiguredChanges(t *testing.T) {
	t.Parallel()

	row := Row{"  Alice  ", "", " HELSINKI "}
	got := NormalizeRow(row, NormalizationConfig{TrimSpace: true, EmptyAs: "NULL"})
	want := Row{"Alice", "NULL", "HELSINKI"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeRow() = %#v, want %#v", got, want)
	}
	if row[0] != "  Alice  " {
		t.Fatal("NormalizeRow mutated its input")
	}
}

func TestNormalizeRowPreservesDataByDefault(t *testing.T) {
	t.Parallel()

	row := Row{"  Alice  ", ""}
	got := NormalizeRow(row, NormalizationConfig{})

	if !reflect.DeepEqual(got, row) {
		t.Fatalf("NormalizeRow() = %#v, want %#v", got, row)
	}
}

func TestNormalizeHeaderRemovesBOMAndNormalizesNames(t *testing.T) {
	t.Parallel()

	got, err := NormalizeHeader(Row{"\ufeff Name ", "POST CODE"}, HeaderConfig{
		TrimSpace: true,
		Case:      HeaderCaseLower,
		Replace:   map[string]string{"post code": "postal_code"},
	})
	if err != nil {
		t.Fatalf("NormalizeHeader() error = %v", err)
	}

	want := Row{"name", "postal_code"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeHeader() = %#v, want %#v", got, want)
	}
}

func TestNormalizeHeaderRejectsEmptyAndDuplicateNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		header Row
		kind   ErrorKind
	}{
		{name: "empty", header: Row{"name", " "}, kind: ErrorInvalidHeader},
		{name: "duplicate", header: Row{"name", "Name"}, kind: ErrorDuplicateHeader},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := NormalizeHeader(test.header, HeaderConfig{
				TrimSpace:        true,
				Case:             HeaderCaseLower,
				RejectEmpty:      true,
				RejectDuplicates: true,
			})
			if !errors.Is(err, test.kind) {
				t.Fatalf("NormalizeHeader() error = %v, want kind %v", err, test.kind)
			}

			var tabularErr *Error
			if !errors.As(err, &tabularErr) {
				t.Fatalf("NormalizeHeader() error type = %T, want *Error", err)
			}
			if tabularErr.Field != 2 {
				t.Fatalf("error field = %d, want 2", tabularErr.Field)
			}
		})
	}
}

func TestErrorExposesStableKindAndContext(t *testing.T) {
	t.Parallel()

	cause := errors.New("bad record")
	err := &Error{
		Kind:   ErrorMalformedRow,
		Op:     "delimited.read",
		Format: "csv",
		Row:    4,
		Field:  2,
		Err:    cause,
	}

	if !errors.Is(err, ErrorMalformedRow) {
		t.Fatal("errors.Is() did not match the stable error kind")
	}
	if !errors.Is(err, cause) {
		t.Fatal("errors.Is() did not match the wrapped cause")
	}
	if got, want := err.Error(), "tabular: delimited.read csv row 4 field 2: malformed row: bad record"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestErrorOmitsUnsetContext(t *testing.T) {
	t.Parallel()

	err := &Error{Kind: ErrorInvalidConfig, Op: "fixedwidth.new"}
	if got, want := err.Error(), "tabular: fixedwidth.new: invalid configuration"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if err.Unwrap() != nil {
		t.Fatalf("Unwrap() = %v, want nil", err.Unwrap())
	}
}
