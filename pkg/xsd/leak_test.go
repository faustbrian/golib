package xsd_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/xsd/compile"
	"github.com/faustbrian/golib/pkg/xsd/validate"
	"go.uber.org/goleak"
)

func TestCompileAndValidateDoNotLeakGoroutines(t *testing.T) {
	defer goleak.VerifyNone(t)

	compiler, err := compile.New(compile.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set, err := compiler.Compile(context.Background(), compile.Source{
		URI: "https://example.test/leak.xsd",
		Content: []byte(`<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema">
 <xs:element name="value" type="xs:string"/>
</xs:schema>`),
	})
	if err != nil {
		t.Fatal(err)
	}
	validator, err := validate.New(set, validate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := validator.Validate(context.Background(), []byte(`<value>ok</value>`))
	if err != nil || !result.Valid {
		t.Fatalf("Validate() = %#v, %v", result, err)
	}
}
