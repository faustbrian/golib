package httpclient

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// IndexedPaginationPage is one page for numeric page and offset strategies.
type IndexedPaginationPage[Item any] struct {
	Items         []Item
	HasNext       bool
	ResponseBytes int64
}

// PageNumberPaginationFetcher loads one positive page number.
type PageNumberPaginationFetcher[Item any] func(
	context.Context,
	int,
) (IndexedPaginationPage[Item], error)

// PageNumberPaginationOptions configures page-number iteration.
type PageNumberPaginationOptions[Item any] struct {
	InitialPage int
	Fetch       PageNumberPaginationFetcher[Item]
	Limits      PaginationLimits
	Clock       RetryClock
	Resume      *PaginationState[Item, int]
}

// NewPageNumberPaginator creates a lazy page-number iterator.
func NewPageNumberPaginator[Item any](
	options PageNumberPaginationOptions[Item],
) (*Paginator[Item, int], error) {
	if nilLike(options.Fetch) {
		return nil, fmt.Errorf("%w: page-number fetcher is nil", ErrInvalidPagination)
	}
	initial := options.InitialPage
	if initial == 0 {
		initial = 1
	}
	if initial < 1 {
		return nil, fmt.Errorf("%w: initial page must be positive", ErrInvalidPagination)
	}

	return NewPaginator(PaginationOptions[Item, int]{
		Initial: initial,
		Fetch: func(ctx context.Context, page int) (PaginationPage[Item, int], error) {
			result, err := options.Fetch(ctx, page)
			if err != nil {
				return PaginationPage[Item, int]{}, err
			}
			next := page
			if result.HasNext {
				if page == int(^uint(0)>>1) {
					return PaginationPage[Item, int]{}, ErrInvalidPagination
				}
				next++
			}

			return PaginationPage[Item, int]{
				Items: result.Items, Next: next, HasNext: result.HasNext,
				ResponseBytes: result.ResponseBytes,
			}, nil
		},
		Key: func(page int) (string, error) {
			if page < 1 {
				return "", ErrInvalidPagination
			}

			return strconv.Itoa(page), nil
		},
		Limits: options.Limits, Clock: options.Clock, Resume: options.Resume,
	})
}

// OffsetContinuation is an immutable offset/limit position.
type OffsetContinuation struct {
	Offset int
	Limit  int
}

// OffsetPaginationFetcher loads one offset/limit position.
type OffsetPaginationFetcher[Item any] func(
	context.Context,
	OffsetContinuation,
) (IndexedPaginationPage[Item], error)

// OffsetPaginationOptions configures offset/limit iteration.
type OffsetPaginationOptions[Item any] struct {
	InitialOffset int
	Limit         int
	Fetch         OffsetPaginationFetcher[Item]
	Limits        PaginationLimits
	Clock         RetryClock
	Resume        *PaginationState[Item, OffsetContinuation]
}

// NewOffsetPaginator creates a lazy offset/limit iterator.
func NewOffsetPaginator[Item any](
	options OffsetPaginationOptions[Item],
) (*Paginator[Item, OffsetContinuation], error) {
	if nilLike(options.Fetch) {
		return nil, fmt.Errorf("%w: offset fetcher is nil", ErrInvalidPagination)
	}
	if options.InitialOffset < 0 || options.Limit < 1 {
		return nil, fmt.Errorf("%w: offset bounds are invalid", ErrInvalidPagination)
	}
	initial := OffsetContinuation{Offset: options.InitialOffset, Limit: options.Limit}

	return NewPaginator(PaginationOptions[Item, OffsetContinuation]{
		Initial: initial,
		Fetch: func(
			ctx context.Context,
			continuation OffsetContinuation,
		) (PaginationPage[Item, OffsetContinuation], error) {
			result, err := options.Fetch(ctx, continuation)
			if err != nil {
				return PaginationPage[Item, OffsetContinuation]{}, err
			}
			next := continuation
			if result.HasNext {
				if continuation.Offset > int(^uint(0)>>1)-continuation.Limit {
					return PaginationPage[Item, OffsetContinuation]{}, ErrInvalidPagination
				}
				next.Offset += continuation.Limit
			}

			return PaginationPage[Item, OffsetContinuation]{
				Items: result.Items, Next: next, HasNext: result.HasNext,
				ResponseBytes: result.ResponseBytes,
			}, nil
		},
		Key: func(continuation OffsetContinuation) (string, error) {
			if continuation.Offset < 0 || continuation.Limit != options.Limit {
				return "", ErrInvalidPagination
			}

			return strconv.Itoa(continuation.Offset) + ":" + strconv.Itoa(continuation.Limit), nil
		},
		Limits: options.Limits, Clock: options.Clock, Resume: options.Resume,
	})
}

// CursorPaginationPage is one opaque-cursor fetch result.
type CursorPaginationPage[Item any] struct {
	Items         []Item
	NextCursor    string
	HasNext       bool
	ResponseBytes int64
}

// CursorPaginationFetcher loads one opaque cursor without normalization.
type CursorPaginationFetcher[Item any] func(
	context.Context,
	string,
) (CursorPaginationPage[Item], error)

// CursorPaginationOptions configures opaque-cursor iteration.
type CursorPaginationOptions[Item any] struct {
	InitialCursor string
	Fetch         CursorPaginationFetcher[Item]
	Limits        PaginationLimits
	Clock         RetryClock
	Resume        *PaginationState[Item, string]
}

// NewCursorPaginator creates a lazy opaque-cursor iterator.
func NewCursorPaginator[Item any](
	options CursorPaginationOptions[Item],
) (*Paginator[Item, string], error) {
	if nilLike(options.Fetch) {
		return nil, fmt.Errorf("%w: cursor fetcher is nil", ErrInvalidPagination)
	}

	return NewPaginator(PaginationOptions[Item, string]{
		Initial: options.InitialCursor,
		Fetch: func(ctx context.Context, cursor string) (PaginationPage[Item, string], error) {
			result, err := options.Fetch(ctx, cursor)
			if err != nil {
				return PaginationPage[Item, string]{}, err
			}

			return PaginationPage[Item, string]{
				Items: result.Items, Next: result.NextCursor, HasNext: result.HasNext,
				ResponseBytes: result.ResponseBytes,
			}, nil
		},
		Key:    func(cursor string) (string, error) { return cursor, nil },
		Limits: options.Limits, Clock: options.Clock, Resume: options.Resume,
	})
}

// LinkPaginationPage is one RFC Link-header fetch result.
type LinkPaginationPage[Item any] struct {
	Items         []Item
	Link          string
	ResponseBytes int64
}

// LinkPaginationFetcher loads one absolute resolved HTTP reference.
type LinkPaginationFetcher[Item any] func(
	context.Context,
	string,
) (LinkPaginationPage[Item], error)

// LinkPaginationOptions configures RFC Link-header iteration.
type LinkPaginationOptions[Item any] struct {
	InitialURL string
	Fetch      LinkPaginationFetcher[Item]
	Limits     PaginationLimits
	Clock      RetryClock
	Resume     *PaginationState[Item, string]
}

// NewLinkPaginator creates a lazy RFC Link-header iterator.
func NewLinkPaginator[Item any](
	options LinkPaginationOptions[Item],
) (*Paginator[Item, string], error) {
	if nilLike(options.Fetch) {
		return nil, fmt.Errorf("%w: Link fetcher is nil", ErrInvalidPagination)
	}
	initial, err := url.Parse(options.InitialURL)
	if err != nil || !validBaseURL(initial) {
		return nil, fmt.Errorf("%w: initial Link URL is invalid", ErrInvalidPagination)
	}

	return NewPaginator(PaginationOptions[Item, string]{
		Initial: initial.String(),
		Fetch: func(ctx context.Context, reference string) (PaginationPage[Item, string], error) {
			base, _ := url.Parse(reference)
			result, fetchErr := options.Fetch(ctx, reference)
			if fetchErr != nil {
				return PaginationPage[Item, string]{}, fetchErr
			}
			target, hasNext, linkErr := ParseNextLink(result.Link)
			if linkErr != nil {
				return PaginationPage[Item, string]{}, linkErr
			}
			next := reference
			if hasNext {
				candidate, targetErr := url.Parse(target)
				if targetErr != nil || !validRelativeReference(candidate) {
					return PaginationPage[Item, string]{}, ErrInvalidPagination
				}
				resolved := base.ResolveReference(candidate)
				if !validBaseURL(resolved) {
					return PaginationPage[Item, string]{}, ErrInvalidPagination
				}
				next = resolved.String()
			}

			return PaginationPage[Item, string]{
				Items: result.Items, Next: next, HasNext: hasNext,
				ResponseBytes: result.ResponseBytes,
			}, nil
		},
		Key: func(reference string) (string, error) {
			candidate, parseErr := url.Parse(reference)
			if parseErr != nil || !validBaseURL(candidate) {
				return "", ErrInvalidPagination
			}

			return candidate.String(), nil
		},
		Limits: options.Limits, Clock: options.Clock, Resume: options.Resume,
	})
}

// ParseNextLink returns the single link target whose rel parameter contains
// next. It accepts commas inside URI references and quoted parameter values.
func ParseNextLink(value string) (string, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false, nil
	}
	entries, err := splitLinkHeader(value, ',')
	if err != nil {
		return "", false, err
	}
	var next string
	for _, entry := range entries {
		target, relations, parseErr := parseLinkEntry(entry)
		if parseErr != nil {
			return "", false, parseErr
		}
		for _, relation := range relations {
			if strings.EqualFold(relation, "next") {
				if next != "" {
					return "", false, fmt.Errorf("%w: Link has multiple next relations", ErrInvalidPagination)
				}
				next = target
			}
		}
	}

	return next, next != "", nil
}

func splitLinkHeader(value string, separator byte) ([]string, error) {
	var entries []string
	start := 0
	inAngle := false
	inQuote := false
	escaped := false
	for index := 0; index < len(value); index++ {
		character := value[index]
		if escaped {
			escaped = false
			continue
		}
		if inQuote && character == '\\' {
			escaped = true
			continue
		}
		if character == '"' && !inAngle {
			inQuote = !inQuote
			continue
		}
		if !inQuote {
			switch character {
			case '<':
				if inAngle {
					return nil, fmt.Errorf("%w: malformed Link angle brackets", ErrInvalidPagination)
				}
				inAngle = true
			case '>':
				if !inAngle {
					return nil, fmt.Errorf("%w: malformed Link angle brackets", ErrInvalidPagination)
				}
				inAngle = false
			case separator:
				if !inAngle {
					entry := strings.TrimSpace(value[start:index])
					if entry == "" {
						return nil, fmt.Errorf("%w: empty Link entry", ErrInvalidPagination)
					}
					entries = append(entries, entry)
					start = index + 1
				}
			}
		}
	}
	if inAngle || inQuote || escaped {
		return nil, fmt.Errorf("%w: unterminated Link syntax", ErrInvalidPagination)
	}
	entry := strings.TrimSpace(value[start:])
	if entry == "" {
		return nil, fmt.Errorf("%w: empty Link entry", ErrInvalidPagination)
	}

	return append(entries, entry), nil
}

func parseLinkEntry(entry string) (string, []string, error) {
	if len(entry) < 3 || entry[0] != '<' {
		return "", nil, fmt.Errorf("%w: Link target is malformed", ErrInvalidPagination)
	}
	closing := strings.IndexByte(entry, '>')
	if closing < 2 {
		return "", nil, fmt.Errorf("%w: Link target is malformed", ErrInvalidPagination)
	}
	target := entry[1:closing]
	rest := strings.TrimSpace(entry[closing+1:])
	if rest == "" {
		return target, nil, nil
	}
	if rest[0] != ';' {
		return "", nil, fmt.Errorf("%w: Link parameters are malformed", ErrInvalidPagination)
	}
	parameters, err := splitLinkHeader(rest[1:], ';')
	if err != nil {
		return "", nil, err
	}
	var relations []string
	for _, parameter := range parameters {
		name, raw, found := strings.Cut(parameter, "=")
		if !found || strings.TrimSpace(name) == "" {
			return "", nil, fmt.Errorf("%w: Link parameter is malformed", ErrInvalidPagination)
		}
		if !strings.EqualFold(strings.TrimSpace(name), "rel") {
			continue
		}
		value, valueErr := parseLinkParameterValue(strings.TrimSpace(raw))
		if valueErr != nil {
			return "", nil, valueErr
		}
		relations = append(relations, strings.Fields(value)...)
	}

	return target, relations, nil
}

func parseLinkParameterValue(value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("%w: Link parameter value is empty", ErrInvalidPagination)
	}
	if value[0] != '"' {
		return value, nil
	}
	unquoted, err := strconv.Unquote(value)
	if err != nil {
		return "", fmt.Errorf("%w: quoted Link parameter is malformed", ErrInvalidPagination)
	}

	return unquoted, nil
}
