package jsonapi_test

import (
	"fmt"
	"net/url"

	jsonapi "github.com/faustbrian/golib/pkg/jsonapi"
)

func ExampleMarshal() {
	document := jsonapi.Document{Data: jsonapi.ResourceData(
		jsonapi.ResourceObject{
			Type:       "articles",
			ID:         "1",
			Attributes: jsonapi.Attributes{"title": "JSON:API in Go"},
		},
	)}

	payload, err := jsonapi.Marshal(document)
	fmt.Println(string(payload))
	fmt.Println(err)
	// Output:
	// {"data":{"type":"articles","id":"1","attributes":{"title":"JSON:API in Go"}}}
	// <nil>
}

func ExampleParseQuery() {
	values := url.Values{
		"include":          {"author.comments"},
		"fields[articles]": {"title,body"},
		"sort":             {"-createdAt,title"},
	}
	query, err := jsonapi.ParseQuery(values)

	fmt.Println(query.Include)
	fmt.Println(query.Fields["articles"])
	fmt.Println(query.Sort[0].Name, query.Sort[0].Descending)
	fmt.Println(err)
	// Output:
	// [author.comments]
	// [title body]
	// createdAt true
	// <nil>
}

func ExampleNegotiator() {
	negotiator, err := jsonapi.NewNegotiator(
		nil,
		[]string{jsonapi.CursorPaginationProfileURI},
	)
	if err != nil {
		fmt.Println(err)
		return
	}

	selected, err := negotiator.NegotiateAccept(
		jsonapi.MediaTypeJSONAPI + `;profile="` +
			jsonapi.CursorPaginationProfileURI + `"`,
	)
	fmt.Println(selected.ContentType)
	fmt.Println(selected.VaryAccept)
	fmt.Println(err)
	// Output:
	// application/vnd.api+json; profile="http://jsonapi.org/profiles/ethanresnick/cursor-pagination/"
	// true
	// <nil>
}

func ExampleCursorPagination() {
	pagination, err := jsonapi.NewCursorPagination(jsonapi.CursorPaginationConfig{
		DefaultSize: 20,
		MaxSize:     100,
		AllowRange:  true,
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	request, err := pagination.Parse(jsonapi.ParameterFamily{
		"page[size]":  {"25"},
		"page[after]": {"opaque-cursor"},
	})
	fmt.Println(request.Size, request.After, request.Range)
	fmt.Println(err)
	// Output:
	// 25 opaque-cursor false
	// <nil>
}

func ExampleMarshalAtomic() {
	document := jsonapi.AtomicDocument{Operations: []jsonapi.AtomicOperation{{
		Op:   jsonapi.AtomicRemove,
		Href: "/articles/1",
	}}}

	payload, err := jsonapi.MarshalAtomic(document)
	fmt.Println(string(payload))
	fmt.Println(err)
	// Output:
	// {"atomic:operations":[{"op":"remove","href":"/articles/1"}]}
	// <nil>
}
