package match

import (
	"fmt"

	"github.com/faustbrian/golib/pkg/international/locale"
	localized "github.com/faustbrian/golib/pkg/localized"
)

// CandidateKind selects exact lookup or locale-layer parent traversal.
type CandidateKind uint8

const (
	// ExactLocale checks only the candidate's canonical tag.
	ExactLocale CandidateKind = iota
	// ParentRange walks Tag.Parent according to the locale dependency.
	ParentRange
)

// Candidate is one ordered configured fallback operation.
type Candidate struct {
	Kind   CandidateKind
	Locale locale.Tag
}

// Chain configures candidates to try after From is absent.
type Chain struct {
	From       locale.Tag
	Candidates []Candidate
}

// PlanOptions bounds a graph and provides an optional final default.
type PlanOptions struct {
	Default       *locale.Tag
	MaxDepth      int
	MaxCandidates int
	Observer      Observer
}

// Plan is an immutable validated fallback graph.
type Plan struct {
	chains     map[string]Chain
	defaultTag *locale.Tag
	observer   Observer
	candidates int
}

// NewPlan validates, bounds, copies, and cycle-checks fallback chains.
func NewPlan(chains []Chain, options PlanOptions) (Plan, error) {
	if options.MaxDepth <= 0 {
		return Plan{}, ErrDepthLimit
	}
	if options.MaxCandidates < 0 {
		return Plan{}, ErrCandidateLimit
	}
	owned := make(map[string]Chain, len(chains))
	total := 0
	for _, chain := range chains {
		canonicalFrom, err := chain.From.Canonical()
		if err != nil {
			return Plan{}, ErrInvalidCandidate
		}
		chain.From = canonicalFrom
		key := canonicalFrom.String()
		if _, exists := owned[key]; exists {
			return Plan{}, ErrDuplicateCandidate
		}
		copy := Chain{From: chain.From, Candidates: append([]Candidate(nil), chain.Candidates...)}
		seen := make(map[string]struct{}, len(copy.Candidates))
		for i, candidate := range copy.Candidates {
			if candidate.Kind > ParentRange {
				return Plan{}, ErrInvalidCandidate
			}
			canonical, err := candidate.Locale.Canonical()
			if err != nil {
				return Plan{}, ErrInvalidCandidate
			}
			candidate.Locale = canonical
			copy.Candidates[i] = candidate
			candidateKey := fmt.Sprintf("%d:%s", candidate.Kind, candidate.Locale.String())
			if _, exists := seen[candidateKey]; exists {
				return Plan{}, ErrDuplicateCandidate
			}
			seen[candidateKey] = struct{}{}
			total++
			if total > options.MaxCandidates {
				return Plan{}, ErrCandidateLimit
			}
		}
		owned[key] = copy
	}
	if err := validateGraph(owned, options.MaxDepth); err != nil {
		return Plan{}, err
	}
	var defaultTag *locale.Tag
	if options.Default != nil {
		copy, err := options.Default.Canonical()
		if err != nil {
			return Plan{}, ErrInvalidCandidate
		}
		defaultTag = &copy
	}
	return Plan{
		chains: owned, defaultTag: defaultTag,
		observer: options.Observer, candidates: total,
	}, nil
}

func validateGraph(chains map[string]Chain, maxDepth int) error {
	const (
		unseen uint8 = iota
		active
		done
	)
	states := make(map[string]uint8, len(chains))
	var visit func(string, int) error
	visit = func(key string, depth int) error {
		if depth > maxDepth {
			return ErrDepthLimit
		}
		if states[key] == active {
			return ErrFallbackCycle
		}
		if states[key] == done {
			return nil
		}
		states[key] = active
		for _, candidate := range chains[key].Candidates {
			next := candidate.Locale.String()
			if candidate.Kind == ParentRange && next == key {
				continue
			}
			if _, exists := chains[next]; exists {
				if err := visit(next, depth+1); err != nil {
					return err
				}
			}
		}
		states[key] = done
		return nil
	}
	for key := range chains {
		if err := visit(key, 1); err != nil {
			return err
		}
	}
	return nil
}

// Resolve checks the requested locale, traverses its configured graph, and
// finally checks the optional default without changing the source value.
func (p Plan) Resolve(value localized.Text, requested locale.Tag) Result {
	canonicalRequested, err := requested.Canonical()
	if err != nil {
		return Result{Kind: Missing}
	}
	requested = canonicalRequested
	var result Result
	if text, ok := value.Get(requested); ok {
		result = present(Exact, requested, requested, text)
		notify(p.observer, Event{Operation: OperationFallback, Kind: result.Kind, CandidateCount: p.candidates})
		return result
	}
	if resolved, ok := p.resolveChain(value, requested, requested); ok {
		result = resolved
		notify(p.observer, Event{Operation: OperationFallback, Kind: result.Kind, CandidateCount: p.candidates})
		return result
	}
	if p.defaultTag != nil {
		if text, ok := value.Get(*p.defaultTag); ok {
			result = present(Default, requested, *p.defaultTag, text)
			notify(p.observer, Event{Operation: OperationFallback, Kind: result.Kind, CandidateCount: p.candidates})
			return result
		}
	}
	result = Result{Kind: Missing, Requested: requested}
	notify(p.observer, Event{Operation: OperationFallback, Kind: result.Kind, CandidateCount: p.candidates})
	return result
}

func (p Plan) resolveChain(value localized.Text, requested, current locale.Tag) (Result, bool) {
	chain, exists := p.chains[current.String()]
	if !exists {
		return Result{}, false
	}
	for _, candidate := range chain.Candidates {
		switch candidate.Kind {
		case ExactLocale:
			if text, ok := value.Get(candidate.Locale); ok {
				return present(Fallback, requested, candidate.Locale, text), true
			}
		case ParentRange:
			for parent, ok := candidate.Locale.Fallback(locale.FallbackParent); ok; parent, ok = parent.Fallback(locale.FallbackParent) {
				if text, ok := value.Get(parent); ok {
					return present(Fallback, requested, parent, text), true
				}
			}
		}
		if candidate.Locale.String() == current.String() {
			continue
		}
		if result, ok := p.resolveChain(value, requested, candidate.Locale); ok {
			return result, true
		}
	}
	return Result{}, false
}
