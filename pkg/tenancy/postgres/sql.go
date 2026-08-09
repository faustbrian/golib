// Package tenancypostgres provides explicit PostgreSQL predicates,
// transaction-local tenant settings, and Row-Level Security plans. It never
// rewrites caller queries or installs hidden global state.
package tenancypostgres

import (
	"errors"
	"fmt"
	"strings"

	"github.com/faustbrian/golib/pkg/tenancy"
)

const (
	// DefaultSetting is the custom PostgreSQL setting used by Manager and RLS.
	DefaultSetting         = "app.tenant_id"
	maximumIdentifierBytes = 63
	maximumParameter       = 65535
)

var (
	// ErrInvalidIdentifier reports an unsafe PostgreSQL identifier.
	ErrInvalidIdentifier = errors.New("tenancy postgres: invalid identifier")
	// ErrInvalidParameter reports an invalid PostgreSQL placeholder position.
	ErrInvalidParameter = errors.New("tenancy postgres: invalid parameter")
	// ErrInvalidRLSOptions reports incomplete or unsafe RLS configuration.
	ErrInvalidRLSOptions = errors.New("tenancy postgres: invalid RLS options")
)

// QueryPredicate is an explicit tenant predicate and its owned arguments.
type QueryPredicate struct {
	Clause    string
	Arguments []any
}

// Predicate returns a quoted equality predicate for exactly one tenant scope.
func Predicate(scope tenancy.Scope, column string, parameter int) (QueryPredicate, error) {
	if !scope.Valid() || scope.Kind() != tenancy.ScopeTenant {
		return QueryPredicate{}, tenancy.ErrTenantScopeRequired
	}
	quoted, err := quoteIdentifier(column, 1)
	if err != nil {
		return QueryPredicate{}, err
	}
	if parameter < 1 || parameter > maximumParameter {
		return QueryPredicate{}, ErrInvalidParameter
	}
	return QueryPredicate{
		Clause:    quoted + " = $" + fmt.Sprint(parameter),
		Arguments: []any{scope.TenantID().Value()},
	}, nil
}

// RLSOptions identify one table-owned tenant isolation policy.
type RLSOptions struct {
	Table   string
	Column  string
	Policy  string
	Setting string
}

// RLSPlan contains explicit migration statements. Apply them through the
// application's migration owner; this package performs no automatic DDL.
type RLSPlan struct {
	Enable  string
	Force   string
	Create  string
	Drop    string
	Setting string
}

// NewRLSPlan validates and quotes identifiers and creates a fail-closed policy
// that treats missing and reset settings as no tenant.
func NewRLSPlan(options RLSOptions) (RLSPlan, error) {
	table, tableErr := quoteIdentifier(options.Table, 2)
	column, columnErr := quoteIdentifier(options.Column, 1)
	policy, policyErr := quoteIdentifier(options.Policy, 1)
	setting := options.Setting
	if setting == "" {
		setting = DefaultSetting
	}
	if tableErr != nil || columnErr != nil || policyErr != nil || !validSetting(setting) {
		return RLSPlan{}, ErrInvalidRLSOptions
	}
	expression := column + " = NULLIF(current_setting('" + setting + "', true), '')"
	return RLSPlan{
		Enable:  "ALTER TABLE " + table + " ENABLE ROW LEVEL SECURITY",
		Force:   "ALTER TABLE " + table + " FORCE ROW LEVEL SECURITY",
		Create:  "CREATE POLICY " + policy + " ON " + table + " USING (" + expression + ") WITH CHECK (" + expression + ")",
		Drop:    "DROP POLICY IF EXISTS " + policy + " ON " + table,
		Setting: setting,
	}, nil
}

func quoteIdentifier(value string, maximumSegments int) (string, error) {
	segments := strings.Split(value, ".")
	if len(segments) == 0 || len(segments) > maximumSegments {
		return "", ErrInvalidIdentifier
	}
	quoted := make([]string, len(segments))
	for index, segment := range segments {
		if !validIdentifierSegment(segment) {
			return "", ErrInvalidIdentifier
		}
		quoted[index] = `"` + segment + `"`
	}
	return strings.Join(quoted, "."), nil
}

func validIdentifierSegment(value string) bool {
	if len(value) == 0 || len(value) > maximumIdentifierBytes ||
		!asciiLetter(value[0]) && value[0] != '_' {
		return false
	}
	for index := 1; index < len(value); index++ {
		char := value[index]
		if !asciiLetter(char) && (char < '0' || char > '9') && char != '_' {
			return false
		}
	}
	return true
}

func validSetting(value string) bool {
	if len(value) == 0 || len(value) > 64 || !strings.Contains(value, ".") {
		return false
	}
	segments := strings.Split(value, ".")
	for _, segment := range segments {
		if !validIdentifierSegment(segment) {
			return false
		}
	}
	return true
}

func asciiLetter(char byte) bool {
	return char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z'
}
