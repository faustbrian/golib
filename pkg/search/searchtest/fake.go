// Package searchtest provides a deterministic bounded in-memory fake for
// application contract tests. It does not emulate OpenSearch ranking,
// analyzers, geo calculations, highlights, aggregations, suggestions, PITs, or
// cursor consistency and reports those capabilities as unsupported.
package searchtest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/faustbrian/golib/pkg/search"
)

var ErrInvalidFake = errors.New("searchtest: valid bounded limits are required")

type Fake struct {
	mu        sync.RWMutex
	limits    search.Limits
	documents map[documentIdentity]search.Document
	versions  map[documentIdentity]uint64
}

func NewFake(limits search.Limits) (*Fake, error) {
	if limits.Validate() != nil {
		return nil, ErrInvalidFake
	}
	return &Fake{
		limits:    limits,
		documents: make(map[documentIdentity]search.Document),
		versions:  make(map[documentIdentity]uint64),
	}, nil
}

func (f *Fake) Capabilities(ctx context.Context) (search.Capabilities, error) {
	if err := ctx.Err(); err != nil {
		return search.Capabilities{}, err
	}
	return fakeCapabilities(), nil
}

func (f *Fake) Write(ctx context.Context, operation search.WriteOperation, refresh search.RefreshPolicy) (search.ItemOutcome, error) {
	request := search.BulkRequest{Operations: []search.WriteOperation{operation}, Refresh: refresh}
	result, err := f.Bulk(ctx, request)
	if err != nil {
		return search.ItemOutcome{}, err
	}
	return result.Items()[0], nil
}

func (f *Fake) Bulk(ctx context.Context, request search.BulkRequest) (search.BulkResult, error) {
	if err := ctx.Err(); err != nil {
		return search.BulkResult{}, err
	}
	if err := request.Validate(fakeCapabilities(), f.limits); err != nil {
		return search.BulkResult{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	items := make([]search.ItemOutcome, len(request.Operations))
	for position, operation := range request.Operations {
		item := search.ItemOutcome{Position: position, ID: operation.ID, Action: operation.Action, Version: operation.Version}
		key := documentKey(operation.Tenant, operation.Index, operation.ID)
		_, exists := f.documents[key]
		currentVersion, versionExists := f.versions[key]
		if versionExists && operation.Version <= currentVersion {
			item.State, item.Code = search.OutcomeVersionConflict, "external_version_conflict"
			items[position] = item
			continue
		}
		switch operation.Action {
		case search.ActionDelete:
			if !versionExists && len(f.versions) >= f.limits.MaxPages*f.limits.MaxPageItems {
				item.State, item.Code = search.OutcomeRejected, "fake_capacity"
				items[position] = item
				continue
			}
			f.versions[key] = operation.Version
			if !exists {
				item.State = search.OutcomeNotFound
			} else {
				delete(f.documents, key)
				item.State = search.OutcomeApplied
			}
		case search.ActionUpdate:
			if !exists {
				item.State = search.OutcomeNotFound
			} else {
				f.documents[key] = operationDocument(operation)
				f.versions[key] = operation.Version
				item.State = search.OutcomeApplied
			}
		case search.ActionIndex, search.ActionUpsert:
			if !versionExists && len(f.versions) >= f.limits.MaxPages*f.limits.MaxPageItems {
				item.State, item.Code = search.OutcomeRejected, "fake_capacity"
				items[position] = item
				continue
			}
			f.documents[key] = operationDocument(operation)
			f.versions[key] = operation.Version
			item.State = search.OutcomeApplied
		}
		items[position] = item
	}
	return search.NewBulkResult(items)
}

func (f *Fake) Search(ctx context.Context, request search.Request) (search.Result, error) {
	if err := ctx.Err(); err != nil {
		return search.Result{}, err
	}
	if err := request.Validate(fakeCapabilities(), f.limits); err != nil {
		return search.Result{}, err
	}
	if len(request.Sort) != 1 || request.Sort[0].Field != "_id" {
		return search.Result{}, search.ErrUnsupported
	}
	page := request.Page.(search.OffsetPage)

	f.mu.RLock()
	documents := make([]search.Document, 0)
	for _, document := range f.documents {
		if document.Tenant == request.Tenant && document.Index == request.Index {
			copyDocument := document
			copyDocument.Source = append(json.RawMessage(nil), document.Source...)
			documents = append(documents, copyDocument)
		}
	}
	f.mu.RUnlock()
	sort.Slice(documents, func(i, j int) bool {
		if request.Sort[0].Direction == search.Descending {
			return documents[i].ID > documents[j].ID
		}
		return documents[i].ID < documents[j].ID
	})
	hits := make([]search.Hit, 0)
	for _, document := range documents {
		matched, err := matches(request.Query, document.Source)
		if err != nil {
			return search.Result{}, err
		}
		if matched {
			hits = append(hits, search.Hit{Index: document.Index, ID: document.ID, Version: document.Version, Source: document.Source, SortValues: []json.RawMessage{mustJSON(document.ID)}})
		}
	}
	total := len(hits)
	start := min(page.Offset, total)
	end := min(start+page.Size, total)
	return search.NewResult(hits[start:end], search.Total{Value: uint64(total), Relation: search.TotalExact}, nil, nil, search.Diagnostics{Backend: "memory-fake"}, "")
}

func fakeCapabilities() search.Capabilities {
	return search.Capabilities{Boolean: true, Term: true, Prefix: true, Exists: true, Offset: true, ExternalVersion: true, UpdateExisting: true, BulkPartialOutcomes: true}
}

func matches(query search.Query, source json.RawMessage) (bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(source))
	decoder.UseNumber()
	fields := make(map[string]any)
	if err := decoder.Decode(&fields); err != nil {
		return false, err
	}
	return matchesFields(query, fields)
}

func matchesFields(query search.Query, fields map[string]any) (bool, error) {
	switch node := query.(type) {
	case search.MatchAllQuery:
		return true, nil
	case search.TermQuery:
		actual, exists := lookup(fields, node.Field)
		if !exists {
			return false, nil
		}
		expected, _ := json.Marshal(node.Value)
		encoded, err := json.Marshal(actual)
		return err == nil && bytes.Equal(encoded, expected), err
	case search.PrefixQuery:
		actual, exists := lookup(fields, node.Field)
		text, ok := actual.(string)
		return exists && ok && strings.HasPrefix(text, node.Prefix), nil
	case search.ExistsQuery:
		actual, exists := lookup(fields, node.Field)
		return exists && hasIndexedValue(actual), nil
	case search.BoolQuery:
		for _, child := range append(append([]search.Query{}, node.Must...), node.Filter...) {
			matched, err := matchesFields(child, fields)
			if err != nil {
				return false, err
			}
			if !matched {
				return false, nil
			}
		}
		for _, child := range node.MustNot {
			matched, err := matchesFields(child, fields)
			if err != nil {
				return false, err
			}
			if matched {
				return false, nil
			}
		}
		matchedShould := 0
		for _, child := range node.Should {
			matched, err := matchesFields(child, fields)
			if err != nil {
				return false, err
			}
			if matched {
				matchedShould++
			}
		}
		minimumShouldMatch := node.MinimumShouldMatch
		if minimumShouldMatch == 0 && len(node.Should) > 0 && len(node.Must) == 0 && len(node.Filter) == 0 {
			minimumShouldMatch = 1
		}
		return matchedShould >= minimumShouldMatch, nil
	default:
		return false, search.ErrUnsupported
	}
}

func hasIndexedValue(value any) bool {
	if value == nil {
		return false
	}
	values, array := value.([]any)
	if !array {
		return true
	}
	for _, item := range values {
		if hasIndexedValue(item) {
			return true
		}
	}
	return false
}

func lookup(fields map[string]any, path string) (any, bool) {
	current := any(fields)
	for part := range strings.SplitSeq(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func operationDocument(operation search.WriteOperation) search.Document {
	return search.Document{Tenant: operation.Tenant, Index: operation.Index, ID: operation.ID, Version: operation.Version, Source: append(json.RawMessage(nil), operation.Source...)}
}

type documentIdentity struct{ tenant, index, id string }

func documentKey(tenant, index, id string) documentIdentity {
	return documentIdentity{tenant: tenant, index: index, id: id}
}
func mustJSON(value string) json.RawMessage { encoded, _ := json.Marshal(value); return encoded }
