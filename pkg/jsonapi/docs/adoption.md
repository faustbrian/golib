# Adoption guide

This guide describes the boundary between `jsonapi` and a production
service. The package owns protocol representation and validation. Your service
owns routing, authorization, persistence, domain mapping, and response policy.

## 1. Define resource mapping explicitly

Keep mapping near the transport layer:

```go
func articleResource(article Article) jsonapi.ResourceObject {
	return jsonapi.ResourceObject{
		Type: "articles",
		ID:   strconv.FormatInt(article.ID, 10),
		Attributes: jsonapi.Attributes{
			"title":     article.Title,
			"createdAt": article.CreatedAt.Format(time.RFC3339),
		},
	}
}
```

Do not expose persistence structs directly. Explicit mapping makes resource
types, identity formatting, field names, omission, and compatibility reviewable.

## 2. Create shared protocol configuration

Construct immutable parsers once during application startup:

```go
type Protocol struct {
	Codec      *jsonapi.Codec
	Query      *jsonapi.QueryParser
	Negotiator *jsonapi.Negotiator
}

func NewProtocol() (*Protocol, error) {
	codec, err := jsonapi.NewCodec(jsonapi.CodecOptions{})
	if err != nil {
		return nil, err
	}
	query, err := jsonapi.NewQueryParser([]string{"searchTerm"}, nil)
	if err != nil {
		return nil, err
	}
	negotiator, err := jsonapi.NewNegotiator(nil, nil)
	if err != nil {
		return nil, err
	}
	return &Protocol{Codec: codec, Query: query, Negotiator: negotiator}, nil
}
```

The configured values are safe for concurrent request use because decoding
does not mutate their registries. Validator callbacks supplied by an
application must themselves be bounded, deterministic, and concurrency-safe.
The package contains their panics and redacts public error strings, but trusted
diagnostics can still inspect retained callback causes and panic values.

## 3. Integrate a `net/http` endpoint

```go
func (api *API) listArticles(w http.ResponseWriter, r *http.Request) {
	selected, err := api.protocol.Negotiator.NegotiateAccept(
		r.Header.Get("Accept"),
	)
	if err != nil {
		writeProtocolError(w, err)
		return
	}

	query, err := api.protocol.Query.Parse(r.URL.Query())
	if err != nil {
		writeProtocolError(w, err)
		return
	}

	articles, err := api.store.ListArticles(r.Context(), query)
	if err != nil {
		writeApplicationError(w, err)
		return
	}

	resources := make([]jsonapi.ResourceObject, len(articles))
	for index, article := range articles {
		resources[index] = articleResource(article)
	}
	document := jsonapi.Document{Data: jsonapi.ResourceCollection(resources...)}
	payload, err := api.protocol.Codec.Marshal(document)
	if err != nil {
		// A server-constructed invalid document is an internal defect.
		writeInternalError(w, err)
		return
	}

	w.Header().Set("Content-Type", selected.ContentType)
	if selected.VaryAccept {
		w.Header().Add("Vary", "Accept")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}
```

For request bodies, validate content type before reading and choose the correct
context:

```go
if _, err := negotiator.CheckContentType(r.Header.Get("Content-Type")); err != nil {
	writeProtocolError(w, err)
	return
}
payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxDocumentBytes))
if err != nil {
	// Handle size/read failure.
}
document, err := jsonapi.UnmarshalWith(payload, jsonapi.ValidationOptions{
	Context: jsonapi.CreateRequest,
})
```

Always bound body size in the HTTP layer. The package validates complete byte
slices and does not impose an application-specific maximum.

## 4. Map typed errors

Suggested boundary mapping:

| Error | HTTP status |
| --- | --- |
| `NegotiationError` | `Status` (`406` or `415`) |
| `QueryError` | `Status` (`400`) |
| `CursorPaginationError` | `Status` (`400`) |
| `DecodeError` | `400` |
| request `ValidationError` | `400` or a more specific application-selected 4xx |
| server marshal/validation failure | `500` and internal logging |
| `AtomicExecutionError` | map the application cause, preserving operation pointer |

Convert failures to `ErrorObject` values without leaking internal causes. A
`Violation.Path` or `DecodeError.Path` can populate `source.pointer`; a
`QueryError.Parameter` can populate `source.parameter`.

## 5. Apply query intent safely

Parsing proves syntax, not authorization or database support. Maintain
endpoint allowlists:

```go
allowedSort := map[string]string{
	"createdAt": "created_at",
	"title":     "title",
}
for _, field := range query.Sort {
	column, ok := allowedSort[field.Name]
	if !ok {
		return unsupportedSort(field.Name)
	}
	statement.OrderBy(column, field.Descending)
}
```

Apply the same discipline to fields, includes, filters, and custom families.
Never interpolate parsed names directly into SQL.

## 6. Build compound documents

When honoring `include`, populate both relationship linkage and `Included`.
The validator rejects duplicate identities and resources that are not linked
from primary data. If sparse fieldsets remove a relationship, account for the
specification's full-linkage exception in your response-building policy.

## 7. Integrate Cursor Pagination

Create one policy per endpoint, validate the parsed query, then apply a unique
database order:

```go
pagination, err := jsonapi.NewCursorPagination(jsonapi.CursorPaginationConfig{
	DefaultSize: 25,
	MaxSize:     100,
	AllowRange:  false,
	ValidateCursor: decodeAndVerifyCursor,
	ValidateSort:   validateStableArticleSort,
})
request, err := pagination.ParseQuery(query)
```

Fetch enough information to set `prev` and `next`, using `NullLink()` when the
profile requires an explicit null. Validate the final page with `CursorPage`.
Cursor values should be opaque, authenticated if tampering matters, and tied
to the effective unique sort.

## 8. Integrate Atomic Operations

Only expose an Atomic endpoint when the storage adapter can provide the
transaction contract. `ExecuteAtomic` calls `ApplyAtomic` in document order,
rolls back after an operation or commit failure, and returns positional
results after a successful commit.

The HTTP layer must still enforce `POST`, negotiate `AtomicExtensionURI`, map
operation failures to appropriate statuses, and decide between a 200 results
document and a valid 204 response.

## 9. Roll out safely

1. record canonical response fixtures from the existing endpoint;
2. map domain data explicitly and compare semantic JSON;
3. run both validators against representative request/response samples;
4. shadow or canary traffic where possible;
5. monitor typed error codes and pointers;
6. treat output ordering and omission changes as compatibility changes;
7. pin a tagged release before production deployment.
