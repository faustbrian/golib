# Use-case cookbook

## Preserve explicit empty and null values

```go
jsonapi.Document{Data: jsonapi.NullData()}             // "data": null
jsonapi.Document{Data: jsonapi.ResourceCollection()}   // "data": []
jsonapi.ResourceObject{Attributes: jsonapi.Attributes{}} // "attributes": {}
jsonapi.Relationship{Data: jsonapi.NullRelationship()} // "data": null
jsonapi.Relationship{Data: jsonapi.ToMany()}           // "data": []
```

Use `nil` to omit optional containers and constructors to express present
values. The distinction survives round trips.

## Validate without encoding

```go
if err := document.ValidateWith(jsonapi.ValidationOptions{
	Context: jsonapi.Response,
}); err != nil {
	// Inspect *jsonapi.ValidationError.
}
```

## Select request context

```go
options := jsonapi.ValidationOptions{Context: jsonapi.UpdateRequest}
document, err := jsonapi.UnmarshalWith(payload, options)
```

Use `ToOneRelationshipRequest` or `ToManyRelationshipRequest` at relationship
mutation endpoints.

## Map validation failures to error objects

```go
var validationError *jsonapi.ValidationError
if errors.As(err, &validationError) {
	errors := make([]jsonapi.ErrorObject, len(validationError.Violations))
	for index, violation := range validationError.Violations {
		errors[index] = jsonapi.ErrorObject{
			Status: "400",
			Code:   violation.Code,
			Title:  "Invalid document",
			Detail: violation.Message,
			Source: &jsonapi.ErrorSource{Pointer: violation.Path},
		}
	}
}
```

## Preserve arbitrary page and filter parameters

`ParseQuery` validates family names but deliberately preserves semantics:

```go
query, err := jsonapi.ParseQuery(values)
statuses := query.Filter["filter[status]"]
size := query.Page["page[size]"]
```

Copy values into endpoint-specific types and validate allowlists before query
construction.

## Register a mixed-case application family

JSON:API reserves lowercase family names. Implementation-specific names must
contain a non-lowercase character:

```go
parser, err := jsonapi.NewQueryParser([]string{"searchTerm"}, nil)
```

Use extension namespaces for colon-prefixed extension families.

## Emit a self link with applied semantics

```go
mediaType := jsonapi.MediaType{
	Extensions: []string{jsonapi.AtomicExtensionURI},
}
document.Links = jsonapi.Links{
	"self": jsonapi.LinkFromObject(jsonapi.LinkObject{
		Href: "/operations",
		Type: mediaType.String(),
	}),
}
```

## Handle unknown profiles and extensions

- Unknown `ext` values in request content types fail with 415.
- `Accept` candidates with unsupported extensions are ignored.
- Unknown profiles do not alter core semantics; request profiles are retained,
  while response negotiation includes only supported profiles.

## Add per-item cursor metadata

```go
resource.Meta = jsonapi.CursorItemMeta(encodedCursor)
```

To preserve unrelated metadata, merge the returned `page` entry into your meta
map instead of replacing the entire map.

## Use local identities in Atomic Operations

An add operation can create a resource using `lid`; later operations may refer
to that identity. Keep operations in dependency order. The Atomic validator
rejects a local reference before its defining add operation.

## Detect typed causes

All protocol errors support normal `errors.As`; `DecodeError` unwraps its
parser cause and `AtomicExecutionError` unwraps both primary and rollback
causes for `errors.Is`/`errors.As`.
