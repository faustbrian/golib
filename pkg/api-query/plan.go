package apiquery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Plan is an immutable, reviewed query description. Accessors return copies so
// adapters cannot mutate a plan after compilation.
type Plan struct {
	resource        string
	revision        string
	responseFields  []string
	executionFields []string
	includes        []string
	filter          *FilterExpr
	constraints     []Constraint
	sorts           []SortTerm
	page            PageRequest
	cursor          *CursorState
	cost            int
	costExceeded    bool
	maxCanonical    int
}

// Resource reports the schema resource identity.
func (p *Plan) Resource() string { return p.resource }

// SchemaRevision reports the exact schema revision used to compile the plan.
func (p *Plan) SchemaRevision() string { return p.revision }

// ResponseFields returns only fields authorized for response projection.
func (p *Plan) ResponseFields() []string {
	return append([]string(nil), p.responseFields...)
}

// ExecutionFields returns response fields plus required server-only fields.
func (p *Plan) ExecutionFields() []string {
	return append([]string(nil), p.executionFields...)
}

// Includes returns a defensive copy of reviewed relationship paths.
func (p *Plan) Includes() []string { return append([]string(nil), p.includes...) }

// Filter returns a deep copy of the reviewed filter expression.
func (p *Plan) Filter() *FilterExpr { return cloneFilter(p.filter) }

// MandatoryConstraints returns server-owned predicates that persistence
// adapters must always compose with client filters.
func (p *Plan) MandatoryConstraints() []Constraint {
	return append([]Constraint(nil), p.constraints...)
}

// Sorts returns a defensive copy of the deterministic ordered sort terms.
func (p *Plan) Sorts() []SortTerm { return append([]SortTerm(nil), p.sorts...) }

// Page returns the bounded page request.
func (p *Plan) Page() PageRequest { return p.page }

// Cursor returns a defensive copy of authenticated cursor state, or nil for a
// first cursor page and non-cursor plans.
func (p *Plan) Cursor() *CursorState {
	if p.cursor == nil {
		return nil
	}
	return &CursorState{Direction: p.cursor.Direction,
		Positions: append([]Value(nil), p.cursor.Positions...), Policy: p.cursor.Policy}
}

// Cost returns the conservative schema-defined projected cost.
func (p *Plan) Cost() int { return p.cost }

// Canonical returns deterministic JSON suitable for cache keys, signing, and
// equality tests. It contains no maps and preserves semantically ordered lists.
func (p *Plan) Canonical() ([]byte, error) {
	encoded, _ := json.Marshal(struct {
		Resource        string        `json:"resource"`
		Revision        string        `json:"revision"`
		ResponseFields  []string      `json:"response_fields"`
		ExecutionFields []string      `json:"execution_fields"`
		Includes        []string      `json:"includes"`
		Filter          *FilterExpr   `json:"filter,omitempty"`
		Constraints     []Constraint  `json:"mandatory_constraints"`
		Sorts           []SortTerm    `json:"sorts"`
		Page            canonicalPage `json:"page"`
		Cost            int           `json:"cost"`
	}{p.resource, p.revision, p.responseFields, p.executionFields, p.includes,
		p.filter, p.constraints, p.sorts, canonicalizePage(p.page, p.cursor), p.cost})
	if len(encoded) > p.maxCanonical {
		return nil, &Violations{items: []Violation{{
			Code: CodeLimitExceeded, Path: "plan", Message: "canonical plan exceeds its size limit",
		}}}
	}
	return encoded, nil
}

type canonicalPage struct {
	Mode         PageMode `json:"mode"`
	Size         int      `json:"size,omitempty"`
	Offset       int      `json:"offset,omitempty"`
	CursorDigest string   `json:"cursor_digest,omitempty"`
}

func canonicalizePage(page PageRequest, cursor *CursorState) canonicalPage {
	result := canonicalPage{Mode: page.Mode, Size: page.Size, Offset: page.Offset}
	if cursor == nil {
		return result
	}
	encoded, _ := json.Marshal(cursor)
	digest := sha256.Sum256(encoded)
	result.CursorDigest = hex.EncodeToString(digest[:])
	return result
}

func cloneFilter(filter *FilterExpr) *FilterExpr {
	if filter == nil {
		return nil
	}
	result := &FilterExpr{Logic: filter.Logic}
	if filter.Predicate != nil {
		result.Predicate = &Predicate{Name: filter.Predicate.Name,
			Operator: filter.Predicate.Operator,
			Values:   append([]Value(nil), filter.Predicate.Values...)}
	}
	result.Children = make([]FilterExpr, len(filter.Children))
	for index := range filter.Children {
		child := cloneFilter(&filter.Children[index])
		result.Children[index] = *child
	}
	return result
}
