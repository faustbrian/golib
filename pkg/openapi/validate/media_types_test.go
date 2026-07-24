package validate_test

import (
	"context"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/reference"
	"github.com/faustbrian/golib/pkg/openapi/validate"
)

func TestDocumentValidatesOpenAPI30MediaTypeEncodings(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.0.4","info":{"title":"API","version":"1"},
		"paths":{"/items":{"post":{
			"requestBody":{"content":{
				"text/plain":{"schema":{"type":"object","properties":{"known":{"type":"string"}}},
					"encoding":{"known":{}}},
				"multipart/form-data":{"schema":{"type":"object","properties":{"known":{"type":"string"}}},
					"encoding":{"missing":{}}}
			}},
			"responses":{"200":{"description":"ok","content":{
				"application/json":{"schema":{"type":"object","properties":{"known":{"type":"string"}}},
					"encoding":{"known":{}}}
			}}}
		}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/paths/~1items/post/requestBody/content/text~1plain/encoding":                  false,
		"/paths/~1items/post/requestBody/content/multipart~1form-data/encoding/missing": false,
		"/paths/~1items/post/responses/200/content/application~1json/encoding":          false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code != "openapi.media-type.encoding.invalid-context" &&
			diagnostic.Code != "openapi.media-type.encoding.property-missing" {
			continue
		}
		if _, exists := want[diagnostic.InstanceLocation]; exists {
			want[diagnostic.InstanceLocation] = true
		}
	}
	for pointer, found := range want {
		if !found {
			t.Errorf("missing encoding diagnostic at %s: %#v", pointer, report.Diagnostics())
		}
	}
}

func TestDocumentResolvesComposedEncodingProperties(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{"/items":{"post":{"requestBody":{"content":{
					"multipart/form-data":{
						"schema":{"allOf":[
							{"type":"object","properties":{"inline":{"type":"string"}}},
							{"$ref":"schemas.json#/Shared"}
						]},
						"encoding":{"inline":{},"external":{},"missing":{}}
					}
				}},"responses":{"204":{"description":"ok"}}}}}
			}`)
			external := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"Schemas","version":"1"},
				"paths":{},"Shared":{"type":"object","properties":{
					"external":{"type":"string","contentMediaType":"image/png"}
				}}
			}`).Raw()
			options := validate.DefaultOptions()
			options.ReferenceResourceURI =
				"https://api.example.test/openapi.json"
			options.ReferenceResolver = reference.ResolverFunc(func(
				_ context.Context,
				identifier string,
			) (reference.Resource, error) {
				return reference.Resource{
					RetrievalURI: identifier,
					Root:         external,
				}, nil
			})
			report, err := validate.DocumentWithOptions(
				context.Background(), document, options,
			)
			if err != nil {
				t.Fatal(err)
			}
			missing := 0
			contentMediaType := false
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code ==
					"openapi.encoding.content-media-type.nonportable" &&
					diagnostic.InstanceLocation ==
						"/paths/~1items/post/requestBody/content/"+
							"multipart~1form-data/schema/properties/"+
							"external/contentMediaType" {
					contentMediaType = true
				}
				if diagnostic.Code !=
					"openapi.media-type.encoding.property-missing" {
					continue
				}
				missing++
				if diagnostic.InstanceLocation !=
					"/paths/~1items/post/requestBody/content/"+
						"multipart~1form-data/encoding/missing" {
					t.Fatalf("unexpected missing property: %#v", diagnostic)
				}
			}
			if missing != 1 {
				t.Fatalf("missing property diagnostics = %d: %#v",
					missing, report.Diagnostics())
			}
			if version == "3.2.0" && !contentMediaType {
				t.Fatalf("missing external contentMediaType warning: %#v",
					report.Diagnostics())
			}
		})
	}
}

func TestMultipartRequestMediaTypesRequireSchemas(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2",
	} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{"/upload":{"post":{"requestBody":{"content":{
					"multipart/form-data":{}
				}},"responses":{"204":{"description":"ok"}}}}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code == "openapi.media-type.multipart.schema-missing" &&
					diagnostic.InstanceLocation ==
						"/paths/~1upload/post/requestBody/content/multipart~1form-data" {
					return
				}
			}
			t.Fatalf("missing multipart schema diagnostic: %#v", report.Diagnostics())
		})
	}
}

func TestMultipleBinaryFileUploadsRequireMultipartContent(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{"/upload":{"post":{"requestBody":{"content":{
					"application/octet-stream":{"schema":{"type":"object","properties":{
						"files":{"type":"array","items":{
							"type":"string","format":"binary"
						}}
					}}},
					"multipart/form-data":{"schema":{"type":"object","properties":{
						"files":{"type":"array","items":{
							"type":"string","format":"binary"
						}}
					}}}
				}},"responses":{"204":{"description":"ok"}}}}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code !=
					"openapi.media-type.multiple-files.non-multipart" {
					continue
				}
				switch diagnostic.InstanceLocation {
				case "/paths/~1upload/post/requestBody/content/application~1octet-stream":
					found = true
				case "/paths/~1upload/post/requestBody/content/multipart~1form-data":
					t.Fatalf("multipart file array was rejected: %#v", diagnostic)
				}
			}
			if !found {
				t.Fatalf("missing non-multipart file diagnostic: %#v",
					report.Diagnostics())
			}
		})
	}
}

func TestRequestBodiesAcceptMultipleSpecificBinaryMediaTypes(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{"/upload":{"post":{"requestBody":{"content":{
					"image/jpeg":{"schema":{"type":"string","format":"binary"}},
					"image/png":{"schema":{"type":"string","format":"binary"}}
				}},"responses":{"204":{"description":"ok"}}}}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			if !report.Valid() {
				t.Fatalf("specific binary media types were rejected: %#v",
					report.Diagnostics())
			}
		})
	}
}

func TestBinaryRequestContentMayOmitSchema(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"3.1.0", "3.1.1", "3.1.2", "3.2.0"} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{"/upload":{"post":{"requestBody":{"content":{
					"application/octet-stream":{}
				}},"responses":{"204":{"description":"ok"}}}}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			if !report.Valid() {
				t.Fatalf("schema-less binary content was rejected: %#v",
					report.Diagnostics())
			}
		})
	}
}

func TestMultipleFileDetectionResolvesExternalSchemaResources(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/upload":{"post":{"requestBody":{"content":{
			"application/json":{"schema":{"$ref":"schemas.json#/Upload"}}
		}},"responses":{"204":{"description":"ok"}}}}}
	}`)
	schemas := mustDocument(t, `{
		"openapi":"3.2.0","paths":{},"Upload":{
			"type":"object","properties":{"files":{
				"type":"array","items":{"$ref":"binary.json#/File"}
			}}
		}
	}`).Raw()
	binary := mustDocument(t, `{
		"openapi":"3.2.0","paths":{},
		"File":{"type":"string","format":"binary"}
	}`).Raw()
	calls := make(map[string]int)
	options := validate.DefaultOptions()
	options.ReferenceResourceURI = "https://api.example.test/openapi.json"
	options.ReferenceResolver = reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		calls[identifier]++
		switch identifier {
		case "https://api.example.test/schemas.json":
			return reference.Resource{RetrievalURI: identifier, Root: schemas}, nil
		case "https://api.example.test/binary.json":
			return reference.Resource{RetrievalURI: identifier, Root: binary}, nil
		default:
			t.Fatalf("identifier = %q", identifier)
			return reference.Resource{}, nil
		}
	})
	report, err := validate.DocumentWithOptions(
		context.Background(), document, options,
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.media-type.multiple-files.non-multipart" &&
			diagnostic.InstanceLocation ==
				"/paths/~1upload/post/requestBody/content/application~1json" {
			if len(calls) != 2 {
				t.Fatalf("resolved resources = %#v", calls)
			}
			return
		}
	}
	t.Fatalf("missing external file-array diagnostic: %#v", report.Diagnostics())
}

func TestDocumentValidatesContentMediaTypeNamesAcrossVersions(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{"/items":{"post":{
					"requestBody":{"content":{
						"text/*":{"schema":{"type":"string"}},
						"not a media type":{"schema":{"type":"string"}}
					}},"responses":{"200":{"description":"ok","content":{
						"application/json":{"schema":{"type":"string"}}
					}}}
				}}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code == "openapi.media-type.name.invalid" &&
					diagnostic.InstanceLocation ==
						"/paths/~1items/post/requestBody/content/not a media type" {
					return
				}
			}
			t.Fatalf("missing invalid media type name: %#v", report.Diagnostics())
		})
	}
}

func TestDocumentValidatesOpenAPI32EncodingForms(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/items":{"post":{
			"requestBody":{"content":{"application/json":{
				"schema":{"type":"array"},"prefixEncoding":[{}]
			}}},
			"responses":{"204":{"description":"ok"}}
		}}},
		"components":{"mediaTypes":{"Conflict":{
			"schema":{"type":"object","properties":{"item":{"type":"string"}}},
			"encoding":{"missing":{}},"itemEncoding":{}
		}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/paths/~1items/post/requestBody/content/application~1json/prefixEncoding": false,
		"/components/mediaTypes/Conflict":                                          false,
		"/components/mediaTypes/Conflict/encoding/missing":                         false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code != "openapi.media-type.encoding.invalid-context" &&
			diagnostic.Code != "openapi.media-type.encoding.conflict" &&
			diagnostic.Code != "openapi.media-type.encoding.property-missing" {
			continue
		}
		if _, exists := want[diagnostic.InstanceLocation]; exists {
			want[diagnostic.InstanceLocation] = true
		}
	}
	for pointer, found := range want {
		if !found {
			t.Errorf("missing encoding diagnostic at %s: %#v", pointer, report.Diagnostics())
		}
	}
}

func TestOpenAPI32ValidatesReusableMediaTypesInContentContext(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/items":{"post":{"requestBody":{"content":{
			"multipart/form-data":{"$ref":"#/components/mediaTypes/Form"}
		}},"responses":{"200":{"description":"ok","content":{
			"application/json":{"$ref":"#/components/mediaTypes/Named"},
			"text/plain":{"$ref":"#/components/mediaTypes/Prefix"},
			"application/problem+json":{"$ref":"#/components/mediaTypes/Item"},
			"application/xml":{"$ref":"#/components/mediaTypes/Missing"},
			"application/octet-stream":null
		}}}}}},
		"components":{"mediaTypes":{
			"Named":{"schema":{"type":"object","properties":{"id":{"type":"string"}}},
				"encoding":{"id":{}}},
			"Prefix":{"itemSchema":{"type":"string"},"prefixEncoding":[{}]},
			"Item":{"itemSchema":{"type":"string"},"itemEncoding":{}},
			"Form":{"itemSchema":{"type":"string"},"prefixEncoding":[{}]}
		}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"/paths/~1items/post/responses/200/content/application~1json/encoding":                  "openapi.media-type.encoding.invalid-context",
		"/paths/~1items/post/responses/200/content/text~1plain/prefixEncoding":                  "openapi.media-type.encoding.invalid-context",
		"/paths/~1items/post/responses/200/content/application~1problem+json/itemEncoding":      "openapi.media-type.encoding.invalid-context",
		"/paths/~1items/post/requestBody/content/multipart~1form-data/prefixEncoding/0/headers": "openapi.encoding.content-disposition.missing",
	}
	found := make(map[string]bool)
	for _, diagnostic := range report.Diagnostics() {
		code, tracked := want[diagnostic.InstanceLocation]
		if tracked && diagnostic.Code == code {
			found[diagnostic.InstanceLocation] = true
		}
	}
	for pointer := range want {
		if !found[pointer] {
			t.Errorf("missing diagnostic at %s: %#v", pointer, report.Diagnostics())
		}
	}
}

func TestOpenAPI32LinksetMediaTypesRequireLinksetSchemas(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/links":{"get":{"responses":{"200":{
			"description":"ok","content":{
				"application/linkset+json":{"schema":{"$ref":"#/components/schemas/Linkset"}},
				"application/linkset":{"schema":{"type":"string"}}
			}
		}}}}},
		"components":{"schemas":{
			"Target":{"type":"object","required":["href"],
				"properties":{"href":{"type":"string","format":"uri-reference"}}},
			"Targets":{"type":"array","items":{"$ref":"#/components/schemas/Target"}},
			"Context":{"type":"object","properties":{"anchor":{"type":"string"}},
				"additionalProperties":{"$ref":"#/components/schemas/Targets"}},
			"Linkset":{"type":"object","required":["linkset"],
				"properties":{"linkset":{"type":"array","items":{"$ref":"#/components/schemas/Context"}}}}
		}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	wantPointer := "/paths/~1links/get/responses/200/content/application~1linkset/schema"
	count := 0
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code != "openapi.media-type.linkset.schema" {
			continue
		}
		count++
		if diagnostic.InstanceLocation != wantPointer {
			t.Fatalf("unexpected linkset diagnostic: %#v", diagnostic)
		}
	}
	if count != 1 {
		t.Fatalf("linkset diagnostics = %d: %#v", count, report.Diagnostics())
	}
}

func TestOpenAPI32IgnoresEncodingFieldsForUnsupportedMediaTypes(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/items":{"get":{"responses":{"200":{
			"description":"ok","content":{
				"application/json":{"encoding":{"value":{}}},
				"text/plain":{"itemSchema":{"type":"string"},
					"prefixEncoding":[{}]}
			}
		}}}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/paths/~1items/get/responses/200/content/application~1json/encoding": false,
		"/paths/~1items/get/responses/200/content/text~1plain/prefixEncoding": false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code != "openapi.media-type.encoding.invalid-context" {
			continue
		}
		if diagnostic.Severity != validate.SeverityWarning {
			t.Fatalf("ignored field diagnostic = %#v", diagnostic)
		}
		if _, exists := want[diagnostic.InstanceLocation]; !exists {
			t.Fatalf("unexpected diagnostic = %#v", diagnostic)
		}
		want[diagnostic.InstanceLocation] = true
	}
	for pointer, found := range want {
		if !found {
			t.Errorf("missing warning at %s: %#v", pointer, report.Diagnostics())
		}
	}
	if !report.Valid() {
		t.Fatalf("ignored fields made document invalid: %#v", report.Diagnostics())
	}
}

func TestOpenAPI32PositionalEncodingRequiresAnItemSchemaOrArraySchema(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{},"components":{
			"schemas":{"Sequence":{"type":"array"}},
			"mediaTypes":{
				"Missing":{"prefixEncoding":[{}]},
				"Object":{"schema":{"type":"object"},"itemEncoding":{}},
				"Items":{"itemSchema":{"type":"string"},"prefixEncoding":[{}]},
				"Array":{"schema":{"type":"array"},"itemEncoding":{}},
				"ArrayKeyword":{"schema":{"items":{}},"itemEncoding":{}},
				"Referenced":{"schema":{"$ref":"#/components/schemas/Sequence"},
					"prefixEncoding":[{}]},
				"External":{"schema":{"$ref":"external.yaml#/Sequence"},
					"prefixEncoding":[{}]}
			}
		}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/components/mediaTypes/Missing/prefixEncoding": false,
		"/components/mediaTypes/Object/itemEncoding":    false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code !=
			"openapi.media-type.positional-encoding.schema-missing" {
			continue
		}
		if _, exists := want[diagnostic.InstanceLocation]; !exists {
			t.Fatalf("unexpected diagnostic = %#v", diagnostic)
		}
		want[diagnostic.InstanceLocation] = true
	}
	for pointer, found := range want {
		if !found {
			t.Errorf("missing diagnostic at %s: %#v", pointer, report.Diagnostics())
		}
	}
}

func TestPositionalEncodingSchemaRuleDoesNotApplyBeforeOpenAPI32(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.1.2","info":{"title":"API","version":"1"},
		"paths":{"/items":{"post":{"requestBody":{"content":{
			"multipart/mixed":{"prefixEncoding":[{}]}
		}},"responses":{"204":{"description":"ok"}}}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code ==
			"openapi.media-type.positional-encoding.schema-missing" {
			t.Fatalf("OpenAPI 3.2 rule applied to 3.1: %#v", diagnostic)
		}
	}
}

func TestOpenAPI32FormDataPositionalEncodingsRequireContentDisposition(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/upload":{"post":{"requestBody":{"content":{
			"multipart/form-data":{
				"itemSchema":{"type":"string"},
				"prefixEncoding":[
					{"headers":{"content-disposition":{"schema":{"type":"string"}}}},
					{"headers":{"X-Meta":{"schema":{"type":"string"}}}},
					{}
				],
				"itemEncoding":{"headers":{}}
			}
		}},"responses":{"204":{"description":"ok"}}}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/paths/~1upload/post/requestBody/content/multipart~1form-data/prefixEncoding/1/headers": false,
		"/paths/~1upload/post/requestBody/content/multipart~1form-data/prefixEncoding/2/headers": false,
		"/paths/~1upload/post/requestBody/content/multipart~1form-data/itemEncoding/headers":     false,
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code !=
			"openapi.encoding.content-disposition.missing" {
			continue
		}
		if _, exists := want[diagnostic.InstanceLocation]; !exists {
			t.Fatalf("unexpected diagnostic = %#v", diagnostic)
		}
		want[diagnostic.InstanceLocation] = true
	}
	for pointer, found := range want {
		if !found {
			t.Errorf("missing diagnostic at %s: %#v", pointer, report.Diagnostics())
		}
	}
}

func TestOpenAPI32SupportsNamedEncodingForNonFormDataMultipart(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/upload":{"post":{"requestBody":{"content":{
			"multipart/mixed":{
				"schema":{"type":"object","properties":{
					"part":{"type":"string"}
				}},
				"encoding":{"part":{"contentType":"text/plain"}}
			}
		}},"responses":{"204":{"description":"ok"}}}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.media-type.encoding.invalid-context" {
			t.Fatalf("named multipart encoding was ignored: %#v", diagnostic)
		}
	}
	if !report.Valid() {
		t.Fatalf("named multipart encoding rejected: %#v", report.Diagnostics())
	}
}

func TestOpenAPI32AcceptsExtensionRegistryMediaTypes(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/items":{"get":{"responses":{"200":{
			"description":"ok","content":{
				"application/vnd.example.future+json":{
					"schema":{"type":"object"}
				}
			}
		}}}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.media-type.name.invalid" {
			t.Fatalf("extension registry media type rejected: %#v", diagnostic)
		}
	}
}

func TestDocumentWarnsAboutEncodingHeadersOutsideMultipart(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{"/items":{"post":{
					"requestBody":{"content":{"application/x-www-form-urlencoded":{
						"schema":{"type":"object","properties":{"item":{"type":"string"}}},
						"encoding":{"item":{"headers":{
							"X-Meta":{"schema":{"type":"string"}}
						}}}
					}}},
					"responses":{"204":{"description":"ok"}}
				}}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code == "openapi.encoding.headers.ignored" &&
					diagnostic.InstanceLocation ==
						"/paths/~1items/post/requestBody/content/application~1x-www-form-urlencoded/encoding/item/headers" &&
					diagnostic.Severity == validate.SeverityWarning {
					return
				}
			}
			t.Fatalf("missing ignored encoding headers warning: %#v", report.Diagnostics())
		})
	}
}

func TestDocumentWarnsAboutEncodingContentTypeOverrides(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{"/items":{"post":{
					"requestBody":{"content":{"multipart/form-data":{
						"schema":{"type":"object","properties":{
							"styled":{"type":"string"},"exploded":{"type":"string"},
							"reserved":{"type":"string"},"plain":{"type":"string"}
						}},
						"encoding":{
							"styled":{"contentType":"text/plain","style":"form"},
							"exploded":{"contentType":"text/plain","explode":true},
							"reserved":{"contentType":"text/plain","allowReserved":true},
							"plain":{"contentType":"text/plain"}
						}
					}}},
					"responses":{"204":{"description":"ok"}}
				}}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			found := make(map[string]bool)
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code != "openapi.encoding.content-type.ignored" {
					continue
				}
				if diagnostic.Severity != validate.SeverityWarning {
					t.Errorf("ignored contentType severity = %s", diagnostic.Severity)
				}
				found[diagnostic.InstanceLocation] = true
			}
			base := "/paths/~1items/post/requestBody/content/multipart~1form-data/encoding/"
			want := []string{"styled", "exploded", "reserved"}
			if strings.HasPrefix(version, "3.0.") {
				want = nil
			}
			for _, name := range want {
				if !found[base+name+"/contentType"] {
					t.Errorf("missing %s contentType warning: %#v", name, report.Diagnostics())
				}
			}
			if found[base+"plain/contentType"] || len(found) != len(want) {
				t.Errorf("unexpected ignored contentType warnings: %#v", found)
			}
		})
	}
}

func TestDocumentWarnsAboutContentMediaTypeWithEncoding(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openapi":"3.2.0","info":{"title":"API","version":"1"},
		"paths":{"/items":{"post":{
			"requestBody":{"content":{"multipart/form-data":{
				"schema":{"type":"object","properties":{
					"inline":{"type":"string","contentMediaType":"image/png"},
					"referenced":{"$ref":"#/components/schemas/Image"},
					"plain":{"type":"string","contentMediaType":"text/plain"}
				}},
				"encoding":{"inline":{},"referenced":{"contentType":"image/jpeg"}}
			}}},
			"responses":{"204":{"description":"ok"}}
		}}},
		"components":{"schemas":{"Image":{
			"type":"string","contentMediaType":"image/png"
		}}}
	}`)
	report, err := validate.Document(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	found := make(map[string]bool)
	for _, diagnostic := range report.Diagnostics() {
		if diagnostic.Code == "openapi.encoding.content-media-type.nonportable" {
			if diagnostic.Severity != validate.SeverityWarning {
				t.Errorf("severity = %s", diagnostic.Severity)
			}
			found[diagnostic.InstanceLocation] = true
		}
	}
	base := "/paths/~1items/post/requestBody/content/multipart~1form-data/schema/properties/"
	for _, name := range []string{"inline", "referenced"} {
		if !found[base+name+"/contentMediaType"] {
			t.Errorf("missing %s warning: %#v", name, report.Diagnostics())
		}
	}
	if found[base+"plain/contentMediaType"] || len(found) != 2 {
		t.Errorf("unexpected contentMediaType warnings: %#v", found)
	}
}

func TestDocumentWarnsAboutContentMediaTypeWithEncodingInOpenAPI31(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"3.1.1", "3.1.2"} {
		document := mustDocument(t, `{
			"openapi":"`+version+`","info":{"title":"API","version":"1"},
			"paths":{},"components":{"requestBodies":{"Body":{"content":{
				"multipart/form-data":{
					"schema":{"type":"object","properties":{"image":{
						"type":"string","contentMediaType":"image/png"
					}}},
					"encoding":{"image":{}}
				}
			}}}}
		}`)
		report, err := validate.Document(context.Background(), document)
		if err != nil {
			t.Fatalf("OpenAPI %s: %v", version, err)
		}
		found := false
		for _, diagnostic := range report.Diagnostics() {
			if diagnostic.Code ==
				"openapi.encoding.content-media-type.nonportable" {
				found = true
			}
		}
		if !found {
			t.Errorf("OpenAPI %s diagnostics = %#v", version, report.Diagnostics())
		}
	}
}

func TestDocumentWarnsAboutOpenAPI30InactiveEncodingSerialization(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
	} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			document := mustDocument(t, `{
				"openapi":"`+version+`","info":{"title":"API","version":"1"},
				"paths":{"/items":{"post":{
					"requestBody":{"content":{"multipart/form-data":{
						"schema":{"type":"object","properties":{"item":{"type":"string"}}},
						"encoding":{"item":{
							"style":"form","explode":true,"allowReserved":true
						}}
					}}},
					"responses":{"204":{"description":"ok"}}
				}}}
			}`)
			report, err := validate.Document(context.Background(), document)
			if err != nil {
				t.Fatal(err)
			}
			found := make(map[string]bool)
			for _, diagnostic := range report.Diagnostics() {
				if diagnostic.Code == "openapi.encoding.serialization.ignored" {
					found[diagnostic.InstanceLocation] = true
				}
			}
			base := "/paths/~1items/post/requestBody/content/multipart~1form-data/encoding/item/"
			for _, field := range []string{"style", "explode", "allowReserved"} {
				if !found[base+field] {
					t.Errorf("missing ignored %s warning: %#v", field, report.Diagnostics())
				}
			}
		})
	}
}
