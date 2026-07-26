# Quickstart

## Construct and encode a document

```go
article := jsonapi.ResourceObject{
	Type: "articles",
	ID:   "1",
	Attributes: jsonapi.Attributes{
		"title": "A strict JSON:API response",
	},
	Relationships: jsonapi.Relationships{
		"author": {
			Data: jsonapi.ToOne(jsonapi.Identifier{Type: "people", ID: "9"}),
		},
	},
}

document := jsonapi.Document{
	Data: jsonapi.ResourceData(article),
	Included: []jsonapi.ResourceObject{{
		Type:       "people",
		ID:         "9",
		Attributes: jsonapi.Attributes{"name": "Ada"},
	}},
}

payload, err := jsonapi.Marshal(document)
```

`Marshal` validates before encoding. Invalid linkage, conflicting top-level
members, duplicate resource identities, malformed links, and other document
violations return `*jsonapi.ValidationError`.

## Decode an untrusted document

```go
document, err := jsonapi.Unmarshal(payload)
switch typed := err.(type) {
case nil:
	// Use document.
case *jsonapi.DecodeError:
	log.Printf("invalid JSON at %s: %s", typed.Path, typed.Message)
case *jsonapi.ValidationError:
	for _, violation := range typed.Violations {
		log.Printf("%s: %s", violation.Path, violation.Message)
	}
default:
	log.Printf("unexpected error: %v", err)
}
```

Decoding is strict for JSON:API-defined objects. Duplicate JSON members and
unknown members are rejected. Members beginning with `@` are ignored as the
specification requires. Registered extension members are handled through a
configured codec.

## Validate request-specific shapes

Use a validation context for endpoint-specific resource requirements:

```go
document, err := jsonapi.UnmarshalWith(payload, jsonapi.ValidationOptions{
	Context: jsonapi.CreateRequest,
})
```

Available contexts are:

- `GenericDocument` for context-independent document validation;
- `Response` for server response identity rules;
- `CreateRequest` for resource creation payloads;
- `UpdateRequest` for resource update payloads;
- `ToOneRelationshipRequest` and `ToManyRelationshipRequest` for relationship
  mutation payloads.

## Parse request parameters

```go
query, err := jsonapi.ParseQuery(r.URL.Query())
if err != nil {
	var queryError *jsonapi.QueryError
	if errors.As(err, &queryError) {
		http.Error(w, queryError.Message, queryError.Status)
		return
	}
}

// query.Include, query.Fields, and query.Sort are parsed.
// query.Page and query.Filter remain explicit application-facing families.
```

Register application or extension families when the endpoint supports them:

```go
parser, err := jsonapi.NewQueryParser(
	[]string{"searchTerm"},
	[]string{"version"},
)
query, err := parser.Parse(r.URL.Query())
```

## Negotiate JSON:API media types

```go
negotiator, err := jsonapi.NewNegotiator(
	[]string{jsonapi.AtomicExtensionURI},
	[]string{jsonapi.CursorPaginationProfileURI},
)

requestMedia, err := negotiator.CheckContentType(r.Header.Get("Content-Type"))
selected, err := negotiator.NegotiateAccept(r.Header.Get("Accept"))

w.Header().Set("Content-Type", selected.ContentType)
if selected.VaryAccept {
	w.Header().Add("Vary", "Accept")
}
```

`NegotiationError.Status` is `415` for request content-type failures and `406`
when no acceptable response representation exists.

## Next steps

- [Adoption guide](adoption.md) for endpoint design and error mapping
- [Examples](examples.md) for complete flows
- [Extensions and profiles](extensions-and-profiles.md)
- [API reference](api.md)
