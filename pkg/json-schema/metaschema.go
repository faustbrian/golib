package jsonschema

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"strings"
)

//go:embed testdata/official/meta-schemas/*/*.json testdata/official/meta-schemas/*/*/*.json
var officialMetaSchemas embed.FS

var officialMetaSchemaPaths = map[string]string{
	"http://json-schema.org/draft-03/schema":                       "testdata/official/meta-schemas/draft-03/schema.json",
	"http://json-schema.org/draft-04/schema":                       "testdata/official/meta-schemas/draft-04/schema.json",
	"http://json-schema.org/draft-06/schema":                       "testdata/official/meta-schemas/draft-06/schema.json",
	"http://json-schema.org/draft-07/schema":                       "testdata/official/meta-schemas/draft-07/schema.json",
	"https://json-schema.org/draft/2019-09/schema":                 "testdata/official/meta-schemas/draft2019-09/schema.json",
	"https://json-schema.org/draft/2019-09/meta/core":              "testdata/official/meta-schemas/draft2019-09/meta/core.json",
	"https://json-schema.org/draft/2019-09/meta/applicator":        "testdata/official/meta-schemas/draft2019-09/meta/applicator.json",
	"https://json-schema.org/draft/2019-09/meta/validation":        "testdata/official/meta-schemas/draft2019-09/meta/validation.json",
	"https://json-schema.org/draft/2019-09/meta/meta-data":         "testdata/official/meta-schemas/draft2019-09/meta/meta-data.json",
	"https://json-schema.org/draft/2019-09/meta/format":            "testdata/official/meta-schemas/draft2019-09/meta/format.json",
	"https://json-schema.org/draft/2019-09/meta/content":           "testdata/official/meta-schemas/draft2019-09/meta/content.json",
	"https://json-schema.org/draft/2020-12/schema":                 "testdata/official/meta-schemas/draft2020-12/schema.json",
	"https://json-schema.org/draft/2020-12/meta/core":              "testdata/official/meta-schemas/draft2020-12/meta/core.json",
	"https://json-schema.org/draft/2020-12/meta/applicator":        "testdata/official/meta-schemas/draft2020-12/meta/applicator.json",
	"https://json-schema.org/draft/2020-12/meta/unevaluated":       "testdata/official/meta-schemas/draft2020-12/meta/unevaluated.json",
	"https://json-schema.org/draft/2020-12/meta/validation":        "testdata/official/meta-schemas/draft2020-12/meta/validation.json",
	"https://json-schema.org/draft/2020-12/meta/meta-data":         "testdata/official/meta-schemas/draft2020-12/meta/meta-data.json",
	"https://json-schema.org/draft/2020-12/meta/format-annotation": "testdata/official/meta-schemas/draft2020-12/meta/format-annotation.json",
	"https://json-schema.org/draft/2020-12/meta/content":           "testdata/official/meta-schemas/draft2020-12/meta/content.json",
}

func compileOfficialMetaSchema(dialect Dialect) (*Schema, error) {
	return compileOfficialMetaSchemaFrom(
		dialect,
		officialMetaSchemas,
		officialMetaSchemaPaths,
	)
}

func compileOfficialMetaSchemaFrom(
	dialect Dialect,
	filesystem fs.FS,
	paths map[string]string,
) (*Schema, error) {
	identifier := strings.TrimSuffix(string(dialect), "#")
	path, exists := paths[identifier]
	if !exists {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedDialect, dialect)
	}
	raw, err := fs.ReadFile(filesystem, path)
	if err != nil {
		return nil, err
	}
	compiler := &Compiler{
		dialect: dialect,
		limits:  DefaultLimits(),
		loader:  ResourceLoaderFunc(loadOfficialMetaSchema),
		formats: standardFormats(),
	}
	return compiler.Compile(context.Background(), raw)
}

func loadOfficialMetaSchema(ctx context.Context, identifier string) ([]byte, error) {
	raw, found, err := loadBundledOfficialMetaSchema(ctx, identifier)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("unknown official meta-schema %q", identifier)
	}
	return raw, nil
}

func loadBundledOfficialMetaSchema(
	ctx context.Context,
	identifier string,
) ([]byte, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, true, err
	}
	parsed, err := url.Parse(identifier)
	if err != nil {
		return nil, false, nil
	}
	parsed, err = normalizeURL(parsed)
	if err != nil {
		return nil, false, nil
	}
	parsed.Fragment = ""
	path, exists := officialMetaSchemaPaths[parsed.String()]
	if !exists {
		return nil, false, nil
	}
	raw, err := officialMetaSchemas.ReadFile(path)
	return raw, true, err
}
