package reference

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

// Occurrence identifies one $ref member and its exact source pointer.
type Occurrence struct {
	pointer Pointer
	raw     string
}

// OccurrenceFilter selects $ref members before their values are interpreted.
// Returning false treats the member as ordinary application data.
type OccurrenceFilter func(Pointer, jsonvalue.Value) bool

// Pointer returns the JSON Pointer to the $ref member.
func (occurrence Occurrence) Pointer() Pointer {
	return occurrence.pointer
}

// Raw returns the exact URI-reference spelling.
func (occurrence Occurrence) Raw() string {
	return occurrence.raw
}

type scanNode struct {
	value     jsonvalue.Value
	tokens    []string
	depth     int
	reference bool
}

// Scan walks an immutable JSON value in source order and returns every $ref
// member. It performs no resolution or external I/O.
func Scan(
	ctx context.Context,
	root jsonvalue.Value,
	limits Limits,
) ([]Occurrence, error) {
	return scan(ctx, root, limits, nil)
}

// ScanFiltered walks an immutable JSON value in source order and returns only
// $ref members selected by filter. Rejected members are not required to contain
// strings, allowing callers to exclude reference-shaped application data.
func ScanFiltered(
	ctx context.Context,
	root jsonvalue.Value,
	limits Limits,
	filter OccurrenceFilter,
) ([]Occurrence, error) {
	if filter == nil {
		return nil, errors.New("scan references: nil occurrence filter")
	}
	return scan(ctx, root, limits, filter)
}

func scan(
	ctx context.Context,
	root jsonvalue.Value,
	limits Limits,
	filter OccurrenceFilter,
) ([]Occurrence, error) {
	if ctx == nil {
		return nil, errors.New("scan references: nil context")
	}
	if err := limits.validate(); err != nil {
		return nil, err
	}
	if root.Kind() == jsonvalue.InvalidKind {
		return nil, errors.New("scan references: invalid root value")
	}
	stack := []scanNode{{value: root}}
	visited := 0
	var occurrences []Occurrence
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		last := len(stack) - 1
		node := stack[last]
		stack = stack[:last]
		visited++
		if node.reference {
			pointer := Pointer{tokens: node.tokens}
			if filter != nil && !filter(pointer, node.value) {
				continue
			}
			raw, ok := node.value.Text()
			if !ok {
				return nil, fmt.Errorf(
					"%w at %s",
					ErrInvalidReference,
					pointer.String(),
				)
			}
			occurrences = append(occurrences, Occurrence{
				pointer: Pointer{tokens: append([]string(nil), node.tokens...)},
				raw:     raw,
			})
			continue
		}
		childCount, _ := node.value.Length()
		if !childrenFitBudget(
			childCount, visited, len(stack), node.depth,
			limits.MaxTraversalNodes, limits.MaxTraversalDepth,
		) {
			return nil, ErrLimitExceeded
		}
		stack = appendScanChildren(stack, node)
	}
	return occurrences, nil
}

func appendScanChildren(stack []scanNode, node scanNode) []scanNode {
	switch node.value.Kind() {
	case jsonvalue.ArrayKind:
		elements, _ := node.value.Elements()
		for index, element := range slices.Backward(elements) {
			stack = append(stack, scanNode{
				value:  element,
				tokens: appendToken(node.tokens, strconv.Itoa(index)),
				depth:  node.depth + 1,
			})
		}
	case jsonvalue.ObjectKind:
		members, _ := node.value.Members()
		for _, member := range slices.Backward(members) {
			stack = append(stack, scanNode{
				value:     member.Value,
				tokens:    appendToken(node.tokens, member.Name),
				depth:     node.depth + 1,
				reference: member.Name == "$ref",
			})
		}
	}
	return stack
}

func appendToken(tokens []string, token string) []string {
	result := make([]string, len(tokens)+1)
	copy(result, tokens)
	result[len(tokens)] = token
	return result
}
