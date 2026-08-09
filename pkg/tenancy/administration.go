package tenancy

import (
	"context"
	"errors"
)

const (
	maximumIterationPageSize = 1000
	maximumIterationTenants  = 1_000_000
	maximumCursorBytes       = 256
)

// ErrInvalidIteration reports an unsafe administrative iteration contract.
var ErrInvalidIteration = errors.New("tenancy: invalid administrative iteration")

// TenantPage is one bounded page from a resumable tenant source. NextCursor is
// empty only when the source is complete.
type TenantPage struct {
	Tenants    []TenantID
	NextCursor string
}

// TenantPager is the consumer-defined source for bounded administrative work.
type TenantPager interface {
	ListTenants(context.Context, string, int) (TenantPage, error)
}

// ResumeToken identifies an exact page position. Offset is the next tenant in
// the page at Cursor, allowing retry after partial-page failure.
type ResumeToken struct {
	Cursor string
	Offset int
}

// AdministrativeAudit records system intent before each tenant operation.
// Implementations own durability and redaction. Returning an error fails closed.
type AdministrativeAudit func(context.Context, AdministrativeReason, TenantID) error

// IterationOptions bound work and make audit integration mandatory.
type IterationOptions struct {
	PageSize   int
	MaxTenants int
	Resume     ResumeToken
	Audit      AdministrativeAudit
}

// IterationResult reports this run's progress and exact continuation point.
type IterationResult struct {
	Processed int
	Resume    ResumeToken
	Complete  bool
}

// IterateTenants executes bounded sequential work from an unscoped base
// context. system must be an explicit system scope. Audit callbacks receive a
// system-scoped child; operations receive a fresh tenant-scoped child derived
// from the original base, so tenant state cannot survive between iterations.
func IterateTenants(
	ctx context.Context,
	system Scope,
	source TenantPager,
	options IterationOptions,
	operation func(context.Context, TenantID) error,
) (IterationResult, error) {
	if !validIteration(ctx, system, source, options, operation) {
		return IterationResult{}, ErrInvalidIteration
	}
	systemContext, _ := WithScope(ctx, system)

	result := IterationResult{Resume: options.Resume}
	cursor := options.Resume.Cursor
	offset := options.Resume.Offset
	seen := make(map[TenantID]struct{})
	seenCursors := make(map[string]struct{})
	pages := 0
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if _, repeated := seenCursors[cursor]; repeated || pages == options.MaxTenants+1 {
			return result, ErrInvalidIteration
		}
		seenCursors[cursor] = struct{}{}
		pages++
		page, pageErr := source.ListTenants(ctx, cursor, options.PageSize)
		if pageErr != nil {
			return result, pageErr
		}
		if err := validateTenantPage(page, cursor, offset, options.PageSize, seen); err != nil {
			return result, err
		}
		for index := offset; index < len(page.Tenants); index++ {
			tenant := page.Tenants[index]
			result.Resume = ResumeToken{Cursor: cursor, Offset: index}
			if err := options.Audit(systemContext, system.AdministrativeReason(), tenant); err != nil {
				return result, err
			}
			if err := ctx.Err(); err != nil {
				return result, err
			}
			tenantScope, _ := NewTenantScope(tenant, Metadata{})
			if err := RunScoped(ctx, tenantScope, func(scoped context.Context) error {
				return operation(scoped, tenant)
			}); err != nil {
				return result, err
			}
			result.Processed++
			if result.Processed == options.MaxTenants {
				return boundedIterationResult(result, cursor, index+1, page), nil
			}
		}
		if page.NextCursor == "" {
			result.Resume = ResumeToken{}
			result.Complete = true
			return result, nil
		}
		cursor = page.NextCursor
		offset = 0
		result.Resume = ResumeToken{Cursor: cursor}
	}
}

func validIteration(
	ctx context.Context,
	system Scope,
	source TenantPager,
	options IterationOptions,
	operation func(context.Context, TenantID) error,
) bool {
	if ctx == nil || !system.Valid() || system.Kind() != ScopeSystem || nilLike(source) ||
		operation == nil || options.Audit == nil || options.PageSize <= 0 ||
		options.PageSize > maximumIterationPageSize || options.MaxTenants <= 0 ||
		options.MaxTenants > maximumIterationTenants || options.Resume.Offset < 0 ||
		len(options.Resume.Cursor) > maximumCursorBytes {
		return false
	}
	_, scoped := ScopeFromContext(ctx)
	return !scoped
}

func validateTenantPage(
	page TenantPage,
	cursor string,
	offset int,
	pageSize int,
	seen map[TenantID]struct{},
) error {
	if len(page.Tenants) > pageSize || offset > len(page.Tenants) ||
		len(page.NextCursor) > maximumCursorBytes ||
		page.NextCursor != "" && page.NextCursor == cursor {
		return ErrInvalidIteration
	}
	for _, tenant := range page.Tenants {
		if !tenant.Valid() {
			return ErrInvalidIteration
		}
		if _, exists := seen[tenant]; exists {
			return ErrInvalidIteration
		}
		seen[tenant] = struct{}{}
	}
	return nil
}

func boundedIterationResult(
	result IterationResult,
	cursor string,
	nextOffset int,
	page TenantPage,
) IterationResult {
	if nextOffset < len(page.Tenants) {
		result.Resume = ResumeToken{Cursor: cursor, Offset: nextOffset}
		return result
	}
	if page.NextCursor != "" {
		result.Resume = ResumeToken{Cursor: page.NextCursor}
		return result
	}
	result.Resume = ResumeToken{}
	result.Complete = true
	return result
}
