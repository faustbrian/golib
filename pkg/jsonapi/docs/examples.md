# End-to-end examples

The runnable examples live in [`examples_test.go`](../examples_test.go) and are
executed by `go test ./... -run '^Example'`. They cover:

- constructing and marshaling a resource document;
- parsing include, fieldset, and sort query parameters;
- negotiating the Cursor Pagination profile;
- parsing an endpoint Cursor Pagination request;
- marshaling an Atomic Operations request.

## Resource with relationship and included data

```go
document := jsonapi.Document{
	Data: jsonapi.ResourceData(jsonapi.ResourceObject{
		Type: "articles",
		ID:   "1",
		Attributes: jsonapi.Attributes{
			"title": "Compound documents",
		},
		Relationships: jsonapi.Relationships{
			"author": {
				Links: jsonapi.Links{
					"self":    jsonapi.URI("/articles/1/relationships/author"),
					"related": jsonapi.URI("/articles/1/author"),
				},
				Data: jsonapi.ToOne(jsonapi.Identifier{Type: "people", ID: "9"}),
			},
		},
	}),
	Included: []jsonapi.ResourceObject{{
		Type:       "people",
		ID:         "9",
		Attributes: jsonapi.Attributes{"name": "Ada"},
	}},
}
payload, err := jsonapi.Marshal(document)
```

## JSON:API error document

```go
document := jsonapi.Document{Errors: []jsonapi.ErrorObject{{
	Status: "422",
	Code:   "invalid-title",
	Title:  "Invalid attribute",
	Detail: "title must not be empty",
	Source: &jsonapi.ErrorSource{Pointer: "/data/attributes/title"},
}}}
payload, err := jsonapi.Marshal(document)
```

## Full link object

```go
describedBy := jsonapi.URI("https://example.com/schemas/article")
link := jsonapi.LinkFromObject(jsonapi.LinkObject{
	Href:        "/articles/1",
	Rel:         "self",
	DescribedBy: &describedBy,
	Title:       "Article 1",
	Type:        jsonapi.MediaTypeJSONAPI,
	Hreflang:    jsonapi.LanguageTags("en", "fi"),
	Meta:        jsonapi.Meta{"cache": "hit"},
})
```

## Cursor page

```go
total := int64(200)
pageMeta, err := (jsonapi.CursorPageMeta{Total: &total}).Meta()
page := jsonapi.CursorPage{
	Request: request,
	Links: jsonapi.Links{
		"prev": jsonapi.NullLink(),
		"next": jsonapi.URI("/articles?page[after]=next-cursor"),
	},
	Meta:  pageMeta,
	Items: []jsonapi.Meta{jsonapi.CursorItemMeta("item-cursor")},
	HasMore: true,
}
err = page.Validate()
```

## Atomic transaction adapter

```go
type transaction struct {
	tx *sql.Tx
}

func (tx *transaction) ApplyAtomic(
	ctx context.Context,
	operation jsonapi.AtomicOperation,
) (jsonapi.AtomicResult, error) {
	// Dispatch add/update/remove and return the positional result.
	return applyOperation(ctx, tx.tx, operation)
}

func (tx *transaction) CommitAtomic(ctx context.Context) error {
	return tx.tx.Commit()
}

func (tx *transaction) RollbackAtomic(ctx context.Context) error {
	return tx.tx.Rollback()
}
```

The `AtomicTransactionBeginner` implementation starts the transaction and
returns this adapter. Avoid nesting independent transactions inside
`ApplyAtomic`; atomicity belongs to the single adapter instance.
