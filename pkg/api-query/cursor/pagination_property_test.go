package cursor_test

import (
	"math/rand/v2"
	"slices"
	"sort"
	"testing"
)

type orderedRecord struct {
	id    int
	score *int
}

func TestForwardAndBackwardTraversalProperties(t *testing.T) {
	t.Parallel()

	for seed := uint64(0); seed < 100; seed++ {
		// #nosec G404 -- deterministic randomness is required for repeatable properties.
		random := rand.New(rand.NewPCG(seed, seed+1))
		records := make([]orderedRecord, 64)
		for index := range records {
			score := random.IntN(8)
			records[index] = orderedRecord{id: index, score: &score}
			if random.IntN(5) == 0 {
				records[index].score = nil
			}
		}
		sortRecords(records)
		forward := traverseForward(records, 7)
		backward := traverseBackward(records, 7)
		if !slices.Equal(forward, recordIDs(records)) {
			t.Fatalf("seed %d forward traversal omitted or duplicated rows", seed)
		}
		if !slices.Equal(backward, recordIDs(records)) {
			t.Fatalf("seed %d backward traversal omitted or duplicated rows", seed)
		}
	}
}

func TestSeekTraversalWithInsertsDeletesTiesNullsAndBoundaries(t *testing.T) {
	t.Parallel()

	zero, one := 0, 1
	records := []orderedRecord{{1, &zero}, {2, &zero}, {3, &one}, {4, nil}, {5, nil}}
	sortRecords(records)
	first := forwardPage(records, nil, 2)
	if !slices.Equal(recordIDs(first), []int{1, 2}) {
		t.Fatalf("first page = %v", recordIDs(first))
	}
	cursor := first[len(first)-1]
	minusOne := -1
	records = append(records, orderedRecord{6, &minusOne}, orderedRecord{7, &zero})
	records = slices.DeleteFunc(records, func(record orderedRecord) bool { return record.id == 3 })
	sortRecords(records)
	second := forwardPage(records, &cursor, 8)
	if !slices.Equal(recordIDs(second), []int{7, 4, 5}) {
		t.Fatalf("page after insert/delete = %v", recordIDs(second))
	}
	if page := forwardPage(records, &records[len(records)-1], 2); len(page) != 0 {
		t.Fatalf("last boundary returned %v", recordIDs(page))
	}
	if page := backwardPage(records, &records[0], 2); len(page) != 0 {
		t.Fatalf("first boundary returned %v", recordIDs(page))
	}
}

func traverseForward(records []orderedRecord, size int) []int {
	var result []int
	var after *orderedRecord
	for {
		page := forwardPage(records, after, size)
		if len(page) == 0 {
			return result
		}
		result = append(result, recordIDs(page)...)
		boundary := page[len(page)-1]
		after = &boundary
	}
}

func traverseBackward(records []orderedRecord, size int) []int {
	pages := make([][]orderedRecord, 0)
	var before *orderedRecord
	for {
		page := backwardPage(records, before, size)
		if len(page) == 0 {
			break
		}
		pages = append(pages, page)
		boundary := page[0]
		before = &boundary
	}
	var result []int
	for index := len(pages) - 1; index >= 0; index-- {
		result = append(result, recordIDs(pages[index])...)
	}
	return result
}

func forwardPage(records []orderedRecord, after *orderedRecord, size int) []orderedRecord {
	start := 0
	if after != nil {
		start = sort.Search(len(records), func(index int) bool {
			return compareRecords(records[index], *after) > 0
		})
	}
	end := min(start+size, len(records))
	return records[start:end]
}

func backwardPage(records []orderedRecord, before *orderedRecord, size int) []orderedRecord {
	end := len(records)
	if before != nil {
		end = sort.Search(len(records), func(index int) bool {
			return compareRecords(records[index], *before) >= 0
		})
	}
	start := max(0, end-size)
	return records[start:end]
}

func sortRecords(records []orderedRecord) {
	sort.Slice(records, func(left, right int) bool {
		return compareRecords(records[left], records[right]) < 0
	})
}

func compareRecords(left, right orderedRecord) int {
	if left.score == nil && right.score != nil {
		return 1
	}
	if left.score != nil && right.score == nil {
		return -1
	}
	if left.score != nil && right.score != nil && *left.score != *right.score {
		return *left.score - *right.score
	}
	return left.id - right.id
}

func recordIDs(records []orderedRecord) []int {
	ids := make([]int, len(records))
	for index := range records {
		ids[index] = records[index].id
	}
	return ids
}
