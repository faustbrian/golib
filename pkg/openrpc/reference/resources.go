package reference

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

type resourceRequest struct {
	base  string
	ref   string
	depth int
}

// Resources loads the external document graph needed by the supplied URI
// references under one shared resolver budget. Returned keys are absolute
// document URIs without fragments. Internal references are not returned.
func (resolver *Resolver) Resources(
	ctx context.Context,
	root jsonvalue.Value,
	base string,
	inputs []string,
) (map[string]jsonvalue.Value, error) {
	if resolver == nil || ctx == nil {
		return nil, ErrResolvePolicy
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(inputs) > resolver.policy.MaxReferences {
		return nil, ErrResolveLimit
	}
	rootURI, err := absoluteDocumentURI(base)
	if err != nil {
		return nil, err
	}
	state := resolveState{documents: map[string]jsonvalue.Value{rootURI: root}}
	queue := make([]resourceRequest, 0, len(inputs))
	for _, input := range inputs {
		queue = append(queue, resourceRequest{base: rootURI, ref: input})
	}
	seen := make(map[string]struct{})
	referenceCount := len(inputs)
	for len(queue) != 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		request := queue[0]
		queue = queue[1:]
		if request.depth >= resolver.policy.MaxDepth {
			return nil, ErrResolveLimit
		}
		resolved, err := resolveResourceReference(request.base, request.ref)
		if err != nil {
			return nil, err
		}
		documentURI := uriWithoutFragment(resolved)
		if documentURI == rootURI {
			continue
		}
		if _, duplicate := seen[documentURI]; duplicate {
			continue
		}
		seen[documentURI] = struct{}{}
		document, exists := state.documents[documentURI]
		if !exists {
			document, err = resolver.load(ctx, documentURI, &state)
			if err != nil {
				return nil, err
			}
			state.documents[documentURI] = document
		}
		references, aliases, err := resourceReferences(
			document,
			documentURI,
			resolver.policy.MaxDepth,
			resolver.policy.MaxReferences-referenceCount,
		)
		if err != nil {
			if errors.Is(err, ErrResolveLimit) {
				return nil, ErrResolveLimit
			}
			return nil, ErrInvalidDocument
		}
		referenceCount += len(references)
		for _, alias := range aliases {
			seen[alias] = struct{}{}
		}
		for _, reference := range references {
			queue = append(queue, resourceRequest{
				base: reference.base, ref: reference.ref, depth: request.depth + 1,
			})
		}
	}
	delete(state.documents, rootURI)
	return state.documents, nil
}

type locatedReference struct {
	base string
	ref  string
}

func resourceReferences(
	value jsonvalue.Value,
	base string,
	maxDepth int,
	maxReferences int,
) ([]locatedReference, []string, error) {
	decoder := json.NewDecoder(bytes.NewReader(value.Bytes()))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, nil, err
	}
	references := make([]locatedReference, 0)
	aliases := make([]string, 0)
	if err := walkResourceReferences(
		decoded, base, 0, maxDepth, maxReferences, &references, &aliases,
	); err != nil {
		return nil, nil, err
	}
	slices.SortFunc(references, func(left locatedReference, right locatedReference) int {
		if byBase := strings.Compare(left.base, right.base); byBase != 0 {
			return byBase
		}
		return strings.Compare(left.ref, right.ref)
	})
	sort.Strings(aliases)
	return references, aliases, nil
}

func walkResourceReferences(
	value any,
	base string,
	depth int,
	maxDepth int,
	maxReferences int,
	references *[]locatedReference,
	aliases *[]string,
) error {
	if depth > maxDepth {
		return ErrResolveLimit
	}
	switch typed := value.(type) {
	case map[string]any:
		currentBase := base
		if identifier, ok := typed["$id"].(string); ok && identifier != "" {
			resolved, err := resolveResourceReference(base, identifier)
			if err != nil {
				return err
			}
			currentBase = resolved.String()
			*aliases = append(*aliases, uriWithoutFragment(resolved))
		}
		if reference, ok := typed["$ref"].(string); ok && reference != "" {
			if len(*references) >= maxReferences {
				return ErrResolveLimit
			}
			*references = append(*references, locatedReference{base: currentBase, ref: reference})
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if err := walkResourceReferences(typed[key], currentBase, depth+1, maxDepth, maxReferences, references, aliases); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := walkResourceReferences(child, base, depth+1, maxDepth, maxReferences, references, aliases); err != nil {
				return err
			}
		}
	}
	return nil
}

func resolveResourceReference(base string, input string) (*url.URL, error) {
	if !utf8ValidURI(input) {
		return nil, ErrInvalidReference
	}
	baseURL, err := url.Parse(base)
	if err != nil || !baseURL.IsAbs() {
		return nil, ErrInvalidBase
	}
	referenceURL, err := url.Parse(input)
	if err != nil {
		return nil, ErrInvalidReference
	}
	return baseURL.ResolveReference(referenceURL), nil
}
