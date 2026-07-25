package cursor

// PageMeta makes traversal boundaries and opaque cursors explicit.
type PageMeta struct {
	HasPrevious    bool   `json:"has_previous"`
	HasNext        bool   `json:"has_next"`
	PreviousCursor string `json:"previous_cursor,omitempty"`
	NextCursor     string `json:"next_cursor,omitempty"`
}

// Page is a stable response envelope whose items remain in canonical sort
// order for both forward and backward requests.
type Page[T any] struct {
	Items []T      `json:"items"`
	Meta  PageMeta `json:"page"`
}

// PageEncoder encodes one boundary item for a traversal direction.
type PageEncoder[T any] func(item T, direction Direction) (string, error)

// BuildPage snapshots canonical-order items and encodes only available
// boundaries. Storage adapters determine hasPrevious and hasNext explicitly.
func BuildPage[T any](items []T, hasPrevious, hasNext bool, encode PageEncoder[T]) (Page[T], error) {
	page := Page[T]{Items: append([]T(nil), items...), Meta: PageMeta{
		HasPrevious: hasPrevious, HasNext: hasNext,
	}}
	if len(items) == 0 {
		return page, nil
	}
	if hasPrevious {
		cursor, err := encode(items[0], Backward)
		if err != nil {
			return Page[T]{}, err
		}
		page.Meta.PreviousCursor = cursor
	}
	if hasNext {
		cursor, err := encode(items[len(items)-1], Forward)
		if err != nil {
			return Page[T]{}, err
		}
		page.Meta.NextCursor = cursor
	}
	return page, nil
}
