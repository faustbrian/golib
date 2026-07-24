package httpclient

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestPageNumberPaginatorAdvancesTypedPages(t *testing.T) {
	t.Parallel()

	var pages []int
	paginator, err := NewPageNumberPaginator(PageNumberPaginationOptions[string]{
		InitialPage: 3,
		Fetch: func(_ context.Context, page int) (IndexedPaginationPage[string], error) {
			pages = append(pages, page)

			return IndexedPaginationPage[string]{
				Items: []string{fmt.Sprintf("page-%d", page)}, HasNext: page < 4, ResponseBytes: 1,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("construct page paginator: %v", err)
	}
	if got := collectPaginator(t, paginator); fmt.Sprint(got) != "[page-3 page-4]" || fmt.Sprint(pages) != "[3 4]" {
		t.Fatalf("items = %v, pages = %v", got, pages)
	}
}

func TestOffsetPaginatorUsesStableLimitSteps(t *testing.T) {
	t.Parallel()

	var offsets []OffsetContinuation
	paginator, err := NewOffsetPaginator(OffsetPaginationOptions[int]{
		InitialOffset: 10,
		Limit:         5,
		Fetch: func(_ context.Context, continuation OffsetContinuation) (IndexedPaginationPage[int], error) {
			offsets = append(offsets, continuation)

			return IndexedPaginationPage[int]{Items: []int{continuation.Offset}, HasNext: len(offsets) < 2}, nil
		},
	})
	if err != nil {
		t.Fatalf("construct offset paginator: %v", err)
	}
	if got := collectPaginator(t, paginator); fmt.Sprint(got) != "[10 15]" {
		t.Fatalf("offset items = %v", got)
	}
	if len(offsets) != 2 || offsets[0].Limit != 5 || offsets[1] != (OffsetContinuation{Offset: 15, Limit: 5}) {
		t.Fatalf("offset continuations = %#v", offsets)
	}
}

func TestCursorPaginatorPreservesOpaqueCursorExactly(t *testing.T) {
	t.Parallel()

	initial := "opaque/+== cursor"
	var cursors []string
	paginator, err := NewCursorPaginator(CursorPaginationOptions[string]{
		InitialCursor: initial,
		Fetch: func(_ context.Context, cursor string) (CursorPaginationPage[string], error) {
			cursors = append(cursors, cursor)
			if len(cursors) == 1 {
				return CursorPaginationPage[string]{Items: []string{"first"}, NextCursor: "next/%2B==", HasNext: true}, nil
			}

			return CursorPaginationPage[string]{Items: []string{"second"}}, nil
		},
	})
	if err != nil {
		t.Fatalf("construct cursor paginator: %v", err)
	}
	if got := collectPaginator(t, paginator); fmt.Sprint(got) != "[first second]" || cursors[0] != initial || cursors[1] != "next/%2B==" {
		t.Fatalf("cursor items = %v, cursors = %#v", got, cursors)
	}
}

func TestLinkPaginatorParsesAndResolvesRFCLinks(t *testing.T) {
	t.Parallel()

	var references []string
	paginator, err := NewLinkPaginator(LinkPaginationOptions[string]{
		InitialURL: "https://api.example.test/widgets?page=1",
		Fetch: func(_ context.Context, reference string) (LinkPaginationPage[string], error) {
			references = append(references, reference)
			if len(references) == 1 {
				return LinkPaginationPage[string]{
					Items: []string{"one"},
					Link:  `</widgets?page=2>; rel="next prev", <https://api.example.test/widgets?page=9>; rel="last"`,
				}, nil
			}

			return LinkPaginationPage[string]{Items: []string{"two"}}, nil
		},
	})
	if err != nil {
		t.Fatalf("construct link paginator: %v", err)
	}
	if got := collectPaginator(t, paginator); fmt.Sprint(got) != "[one two]" {
		t.Fatalf("link items = %v", got)
	}
	if fmt.Sprint(references) != "[https://api.example.test/widgets?page=1 https://api.example.test/widgets?page=2]" {
		t.Fatalf("link references = %v", references)
	}
}

func TestParseNextLinkHandlesQuotedCommasAndRejectsAmbiguity(t *testing.T) {
	t.Parallel()

	target, ok, err := ParseNextLink(`<https://api.example.test/a,b>; title="a,b"; rel=next`)
	if err != nil || !ok || target != "https://api.example.test/a,b" {
		t.Fatalf("quoted-comma link = %q, %v, %v", target, ok, err)
	}
	if _, ok, err := ParseNextLink(`<a>; rel=prev`); err != nil || ok {
		t.Fatalf("absent next = %v, %v", ok, err)
	}
	for _, value := range []string{
		`not-a-link`,
		`<a; rel=next`,
		`<a>; rel=next, <b>; rel=next`,
		`<a>; rel="unterminated`,
	} {
		if _, _, err := ParseNextLink(value); !errors.Is(err, ErrInvalidPagination) {
			t.Fatalf("malformed Link %q error = %v", value, err)
		}
	}
}

func TestPaginationStrategiesRejectInvalidConfigurationAndOverflow(t *testing.T) {
	t.Parallel()

	var nilPageFetch PageNumberPaginationFetcher[int]
	var nilOffsetFetch OffsetPaginationFetcher[int]
	var nilCursorFetch CursorPaginationFetcher[int]
	var nilLinkFetch LinkPaginationFetcher[int]
	constructors := []func() error{
		func() error {
			_, err := NewPageNumberPaginator(PageNumberPaginationOptions[int]{InitialPage: -1, Fetch: nilPageFetch})
			return err
		},
		func() error {
			_, err := NewOffsetPaginator(OffsetPaginationOptions[int]{Limit: 0, Fetch: nilOffsetFetch})
			return err
		},
		func() error {
			_, err := NewCursorPaginator(CursorPaginationOptions[int]{Fetch: nilCursorFetch})
			return err
		},
		func() error {
			_, err := NewLinkPaginator(LinkPaginationOptions[int]{InitialURL: ":bad", Fetch: nilLinkFetch})
			return err
		},
	}
	for _, construct := range constructors {
		if err := construct(); !errors.Is(err, ErrInvalidPagination) {
			t.Fatalf("strategy configuration error = %v", err)
		}
	}

	pagePaginator, err := NewPageNumberPaginator(PageNumberPaginationOptions[int]{
		InitialPage: int(^uint(0) >> 1),
		Fetch: func(context.Context, int) (IndexedPaginationPage[int], error) {
			return IndexedPaginationPage[int]{Items: []int{1}, HasNext: true}, nil
		},
	})
	if err != nil {
		t.Fatalf("construct overflow page paginator: %v", err)
	}
	if _, _, err := pagePaginator.Next(context.Background()); !errors.Is(err, ErrInvalidPagination) {
		t.Fatalf("page overflow error = %v", err)
	}

	offsetPaginator, err := NewOffsetPaginator(OffsetPaginationOptions[int]{
		InitialOffset: int(^uint(0)>>1) - 1, Limit: 2,
		Fetch: func(context.Context, OffsetContinuation) (IndexedPaginationPage[int], error) {
			return IndexedPaginationPage[int]{Items: []int{1}, HasNext: true}, nil
		},
	})
	if err != nil {
		t.Fatalf("construct overflow offset paginator: %v", err)
	}
	if _, _, err := offsetPaginator.Next(context.Background()); !errors.Is(err, ErrInvalidPagination) {
		t.Fatalf("offset overflow error = %v", err)
	}
}

func TestPaginationStrategyBoundaryFailures(t *testing.T) {
	t.Parallel()

	failure := errors.New("fetch failure")
	pageFetch := func(context.Context, int) (IndexedPaginationPage[int], error) {
		return IndexedPaginationPage[int]{}, failure
	}
	if _, err := NewPageNumberPaginator(PageNumberPaginationOptions[int]{InitialPage: -1, Fetch: pageFetch}); !errors.Is(err, ErrInvalidPagination) {
		t.Fatalf("negative initial page error = %v", err)
	}
	defaultPage, err := NewPageNumberPaginator(PageNumberPaginationOptions[int]{Fetch: pageFetch})
	if err != nil {
		t.Fatalf("construct default page paginator: %v", err)
	}
	if _, _, err := defaultPage.Next(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("page fetch error = %v", err)
	}
	invalidPageResume := PaginationState[int, int]{Continuation: 0, HasNext: true}
	invalidPage, err := NewPageNumberPaginator(PageNumberPaginationOptions[int]{
		Fetch: func(context.Context, int) (IndexedPaginationPage[int], error) {
			return IndexedPaginationPage[int]{}, nil
		},
		Resume: &invalidPageResume,
	})
	if err != nil {
		t.Fatalf("construct invalid page resume: %v", err)
	}
	if _, _, err := invalidPage.Next(context.Background()); !errors.Is(err, ErrInvalidPagination) {
		t.Fatalf("invalid page continuation error = %v", err)
	}

	offsetFetch := func(context.Context, OffsetContinuation) (IndexedPaginationPage[int], error) {
		return IndexedPaginationPage[int]{}, failure
	}
	if _, err := NewOffsetPaginator(OffsetPaginationOptions[int]{InitialOffset: -1, Limit: 1, Fetch: offsetFetch}); !errors.Is(err, ErrInvalidPagination) {
		t.Fatalf("invalid offset bounds error = %v", err)
	}
	invalidOffsetResume := PaginationState[int, OffsetContinuation]{
		Continuation: OffsetContinuation{Offset: 0, Limit: 2}, HasNext: true,
	}
	invalidOffset, err := NewOffsetPaginator(OffsetPaginationOptions[int]{Limit: 1, Fetch: offsetFetch, Resume: &invalidOffsetResume})
	if err != nil {
		t.Fatalf("construct invalid offset resume: %v", err)
	}
	if _, _, err := invalidOffset.Next(context.Background()); !errors.Is(err, ErrInvalidPagination) {
		t.Fatalf("invalid offset continuation error = %v", err)
	}
	validOffset, err := NewOffsetPaginator(OffsetPaginationOptions[int]{Limit: 1, Fetch: offsetFetch})
	if err != nil {
		t.Fatalf("construct failing offset: %v", err)
	}
	if _, _, err := validOffset.Next(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("offset fetch error = %v", err)
	}

	cursor, err := NewCursorPaginator(CursorPaginationOptions[int]{
		Fetch: func(context.Context, string) (CursorPaginationPage[int], error) {
			return CursorPaginationPage[int]{}, failure
		},
	})
	if err != nil {
		t.Fatalf("construct failing cursor: %v", err)
	}
	if _, _, err := cursor.Next(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("cursor fetch error = %v", err)
	}

	linkFetch := func(context.Context, string) (LinkPaginationPage[int], error) {
		return LinkPaginationPage[int]{}, nil
	}
	if _, err := NewLinkPaginator(LinkPaginationOptions[int]{InitialURL: ":bad", Fetch: linkFetch}); !errors.Is(err, ErrInvalidPagination) {
		t.Fatalf("invalid initial Link URL error = %v", err)
	}
	for _, test := range []struct {
		name   string
		resume string
		fetch  LinkPaginationFetcher[int]
	}{
		{name: "invalid resumed base", resume: ":bad", fetch: linkFetch},
		{name: "fetch failure", fetch: func(context.Context, string) (LinkPaginationPage[int], error) {
			return LinkPaginationPage[int]{}, failure
		}},
		{name: "malformed Link", fetch: func(context.Context, string) (LinkPaginationPage[int], error) {
			return LinkPaginationPage[int]{Link: "malformed"}, nil
		}},
		{name: "userinfo target", fetch: func(context.Context, string) (LinkPaginationPage[int], error) {
			return LinkPaginationPage[int]{Link: `<https://user@example.test/next>; rel=next`}, nil
		}},
		{name: "non HTTP target", fetch: func(context.Context, string) (LinkPaginationPage[int], error) {
			return LinkPaginationPage[int]{Link: `<ftp://example.test/next>; rel=next`}, nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var resume *PaginationState[int, string]
			if test.resume != "" {
				resume = &PaginationState[int, string]{Continuation: test.resume, HasNext: true}
			}
			paginator, err := NewLinkPaginator(LinkPaginationOptions[int]{
				InitialURL: "https://api.example.test/start", Fetch: test.fetch, Resume: resume,
			})
			if err != nil {
				t.Fatalf("construct Link paginator: %v", err)
			}
			if _, _, err := paginator.Next(context.Background()); err == nil {
				t.Fatal("Link boundary accepted")
			}
		})
	}
}

func TestLinkParserBoundaryMatrix(t *testing.T) {
	t.Parallel()

	if target, relations, err := parseLinkEntry(`<a>`); err != nil || target != "a" || len(relations) != 0 {
		t.Fatalf("parameterless Link = %q, %v, %v", target, relations, err)
	}
	if _, _, err := parseLinkEntry(`<a>; title="unterminated`); !errors.Is(err, ErrInvalidPagination) {
		t.Fatalf("direct malformed Link parameters error = %v", err)
	}
	if target, ok, err := ParseNextLink(`<a>; title="escaped\"quote"; rel=next`); err != nil || !ok || target != "a" {
		t.Fatalf("escaped quoted Link = %q, %v, %v", target, ok, err)
	}
	for _, value := range []string{
		`<<a>>; rel=next`,
		`<a>>; rel=next`,
		`, <a>; rel=next`,
		`<a>; rel=next,`,
		`<>; rel=next`,
		`<a> rel=next`,
		`<a>; broken`,
		`<a>; =next`,
		`<a>; rel=`,
		`<a>; rel="\q"`,
	} {
		if _, _, err := ParseNextLink(value); !errors.Is(err, ErrInvalidPagination) {
			t.Fatalf("Link boundary %q error = %v", value, err)
		}
	}
	if target, ok, err := ParseNextLink(`<a>; title=value; rel=next`); err != nil || !ok || target != "a" {
		t.Fatalf("non-rel parameter Link = %q, %v, %v", target, ok, err)
	}
}

func collectPaginator[Item any, Continuation any](
	t *testing.T,
	paginator *Paginator[Item, Continuation],
) []Item {
	t.Helper()
	var items []Item
	for {
		item, ok, err := paginator.Next(context.Background())
		if err != nil {
			t.Fatalf("iterate paginator: %v", err)
		}
		if !ok {
			return items
		}
		items = append(items, item)
	}
}
