package cursor_test

import (
	"fmt"
	"testing"

	"github.com/faustbrian/golib/pkg/api-query/cursor"
)

func TestBuildPageMakesBoundarySemanticsExplicit(t *testing.T) {
	t.Parallel()

	items := []int{10, 20}
	page, err := cursor.BuildPage(items, true, true, func(item int, direction cursor.Direction) (string, error) {
		return fmt.Sprintf("%s:%d", direction, item), nil
	})
	if err != nil {
		t.Fatalf("BuildPage() error = %v", err)
	}
	items[0] = 99
	if len(page.Items) != 2 || page.Items[0] != 10 {
		t.Fatalf("Items = %v, want immutable canonical-order page", page.Items)
	}
	if page.Meta.PreviousCursor != "backward:10" || page.Meta.NextCursor != "forward:20" ||
		!page.Meta.HasPrevious || !page.Meta.HasNext {
		t.Fatalf("Meta = %#v", page.Meta)
	}
}

func TestBuildPageHandlesEmptyBoundary(t *testing.T) {
	t.Parallel()

	page, err := cursor.BuildPage([]string{}, false, false, func(string, cursor.Direction) (string, error) {
		t.Fatal("encoder called for empty page")
		return "", nil
	})
	if err != nil {
		t.Fatalf("BuildPage() error = %v", err)
	}
	if page.Meta.HasNext || page.Meta.HasPrevious || page.Meta.NextCursor != "" || page.Meta.PreviousCursor != "" {
		t.Fatalf("Meta = %#v, want empty boundary", page.Meta)
	}
}
