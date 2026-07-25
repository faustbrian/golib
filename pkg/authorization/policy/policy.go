// Package policy provides snapshot inspection, diff, dry-run, and portable
// policy manifest contracts.
package policy

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sort"

	authorization "github.com/faustbrian/golib/pkg/authorization"
)

var ErrNilSnapshot = errors.New("policy snapshot is nil")

type SnapshotDiff struct {
	FromRevision     authorization.Revision
	ToRevision       authorization.Revision
	AlgorithmChanged bool
	Added            []authorization.PolicyID
	Removed          []authorization.PolicyID
	Changed          []authorization.PolicyID
}

func Diff(current, candidate *authorization.Snapshot) (SnapshotDiff, error) {
	if current == nil || candidate == nil {
		return SnapshotDiff{}, ErrNilSnapshot
	}

	diff := SnapshotDiff{
		FromRevision:     current.Revision(),
		ToRevision:       candidate.Revision(),
		AlgorithmChanged: current.Algorithm() != candidate.Algorithm(),
	}
	currentPolicies := indexPolicies(current.Policies())
	candidatePolicies := indexPolicies(candidate.Policies())

	for id, currentPolicy := range currentPolicies {
		candidatePolicy, exists := candidatePolicies[id]
		if !exists {
			diff.Removed = append(diff.Removed, id)
			continue
		}
		if !policyInfoEqual(currentPolicy, candidatePolicy) {
			diff.Changed = append(diff.Changed, id)
		}
	}
	for id := range candidatePolicies {
		if _, exists := currentPolicies[id]; !exists {
			diff.Added = append(diff.Added, id)
		}
	}

	sortPolicyIDs(diff.Added)
	sortPolicyIDs(diff.Removed)
	sortPolicyIDs(diff.Changed)
	return diff, nil
}

type DecisionComparison struct {
	Index     int
	Current   authorization.Decision
	Candidate authorization.Decision
	Changed   bool
}

type DryRunReport struct {
	FromRevision authorization.Revision
	ToRevision   authorization.Revision
	Decisions    []DecisionComparison
}

func DryRun(
	ctx context.Context,
	current *authorization.Snapshot,
	candidate *authorization.Snapshot,
	requests []authorization.Request,
) (DryRunReport, error) {
	if current == nil || candidate == nil {
		return DryRunReport{}, ErrNilSnapshot
	}

	currentEngine, err := authorization.NewEngine(current)
	if err != nil {
		return DryRunReport{}, err
	}
	candidateEngine, err := authorization.NewEngine(candidate)
	if err != nil {
		return DryRunReport{}, err
	}
	currentDecisions, currentErr := currentEngine.DecideBatch(ctx, requests)
	candidateDecisions, candidateErr := candidateEngine.DecideBatch(ctx, requests)

	report := DryRunReport{
		FromRevision: current.Revision(),
		ToRevision:   candidate.Revision(),
	}
	evaluationErr := errors.Join(currentErr, candidateErr)
	if len(currentDecisions) != len(requests) ||
		len(candidateDecisions) != len(requests) {
		return report, evaluationErr
	}
	report.Decisions = make([]DecisionComparison, len(requests))
	for index := range requests {
		report.Decisions[index] = DecisionComparison{
			Index:     index,
			Current:   currentDecisions[index],
			Candidate: candidateDecisions[index],
			Changed:   decisionsDiffer(currentDecisions[index], candidateDecisions[index]),
		}
	}

	return report, evaluationErr
}

func indexPolicies(policies []authorization.PolicyInfo) map[authorization.PolicyID]authorization.PolicyInfo {
	indexed := make(map[authorization.PolicyID]authorization.PolicyInfo, len(policies))
	for _, policy := range policies {
		indexed[policy.ID] = policy
	}
	return indexed
}

func policyInfoEqual(left, right authorization.PolicyInfo) bool {
	return left.ID == right.ID && left.Revision == right.Revision &&
		left.Priority == right.Priority && left.ActiveFrom.Equal(right.ActiveFrom) &&
		left.ActiveUntil.Equal(right.ActiveUntil) && maps.Equal(left.Metadata, right.Metadata)
}

func decisionsDiffer(left, right authorization.Decision) bool {
	return left.Outcome != right.Outcome || left.Reason != right.Reason ||
		!slices.Equal(left.MatchedPolicyIDs, right.MatchedPolicyIDs) ||
		left.MatchedPolicyIDsTruncated != right.MatchedPolicyIDsTruncated
}

func sortPolicyIDs(ids []authorization.PolicyID) {
	sort.Slice(ids, func(left, right int) bool { return ids[left] < ids[right] })
}
