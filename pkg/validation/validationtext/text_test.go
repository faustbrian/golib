package validationtext_test

import (
	"strings"
	"testing"

	validation "github.com/faustbrian/golib/pkg/validation"
	"github.com/faustbrian/golib/pkg/validation/validationtext"
)

type catalog map[string]string

func (messages catalog) Lookup(locale, code string, _ map[string]string) (string, bool) {
	message, ok := messages[locale+":"+code]
	return message, ok
}

type hostileCatalog struct{ text string }

func (catalog hostileCatalog) Lookup(string, string,
	map[string]string,
) (string, bool) {
	if catalog.text == "panic" {
		panic("token=secret")
	}
	return catalog.text, true
}

func TestMessagesKeepMachineSemanticsSeparateFromTranslation(t *testing.T) {
	ctx, err := validation.NewContext(validation.DefaultLimits(), validation.WithLocale("fi"))
	if err != nil {
		t.Fatal(err)
	}
	report := validation.NewReport(ctx.Limits()).Add(validation.NewViolation(
		validation.RootPath().Append(validation.Field("name")), "required",
		validation.Error, nil, nil,
	))
	messages := validationtext.Messages(ctx, report, catalog{"fi:required": "Pakollinen"})
	if len(messages) != 1 || messages[0].Code != "required" || messages[0].Text != "Pakollinen" {
		t.Fatalf("messages = %#v", messages)
	}
}

func TestMessagesContainAndSanitizeHostileCatalogOutput(t *testing.T) {
	limits := validation.DefaultLimits()
	limits.MaxStringLength = 16
	ctx, err := validation.NewContext(limits, validation.WithLocale("en"))
	if err != nil {
		t.Fatal(err)
	}
	report := validation.NewReport(limits).Add(validation.NewViolation(
		validation.RootPath().Append(validation.Field("password")), "required",
		validation.Error, nil, nil,
	))
	tests := []struct {
		name, want string
		catalog    validationtext.Catalog
	}{
		{"nil", "", nil},
		{"missing", "", catalog{}},
		{"panic", "", hostileCatalog{text: "panic"}},
		{"exact limit", strings.Repeat("x", 16), hostileCatalog{text: strings.Repeat("x", 16)}},
		{"oversized", "", hostileCatalog{text: strings.Repeat("x", 17)}},
		{"invalid utf8", "", hostileCatalog{text: string([]byte{0xff})}},
		{"control", "", hostileCatalog{text: "bad\ntext"}},
		{"markup", "&lt;b&gt;x&lt;/b&gt;", hostileCatalog{text: "<b>x</b>"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messages := validationtext.Messages(ctx, report, tt.catalog)
			if len(messages) != 1 || messages[0].Path != "password" ||
				messages[0].Code != "required" || messages[0].Text != tt.want {
				t.Fatalf("messages = %#v", messages)
			}
		})
	}
}
