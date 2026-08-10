package tenancy

import (
	"errors"
	"fmt"
	"log/slog"
	"maps"
)

const (
	maxMetadataEntries = 32
	maxMetadataKey     = 64
	maxMetadataValue   = 256
	maxActorBytes      = 128
	maxPurposeBytes    = 256
	maxReferenceBytes  = 128
)

var (
	// ErrInvalidMetadata reports malformed or unbounded optional metadata.
	ErrInvalidMetadata = errors.New("tenancy: invalid metadata")
	// ErrCapabilityRequired reports an attempt to construct system scope without
	// an explicit capability.
	ErrCapabilityRequired = errors.New("tenancy: system capability required")
	// ErrInvalidAdministrativeReason reports missing or malformed audit intent.
	ErrInvalidAdministrativeReason = errors.New("tenancy: invalid administrative reason")
)

// Metadata is immutable optional routing metadata. It is copied at every
// ownership boundary and must not carry authorization decisions or secrets.
type Metadata struct {
	values map[string]string
}

// NewMetadata validates and owns a copy of values.
func NewMetadata(values map[string]string) (Metadata, error) {
	if len(values) > maxMetadataEntries {
		return Metadata{}, fmt.Errorf("%w: too many entries", ErrInvalidMetadata)
	}
	owned := make(map[string]string, len(values))
	for key, value := range values {
		if !validMetadataKey(key) || !validPrintable(value, maxMetadataValue, true) {
			return Metadata{}, fmt.Errorf("%w: key or value", ErrInvalidMetadata)
		}
		owned[key] = value
	}
	if len(owned) == 0 {
		return Metadata{}, nil
	}
	return Metadata{values: owned}, nil
}

// Get returns one metadata value.
func (metadata Metadata) Get(key string) (string, bool) {
	value, ok := metadata.values[key]
	return value, ok
}

// Values returns an independently owned copy.
func (metadata Metadata) Values() map[string]string {
	return maps.Clone(metadata.values)
}

// String returns a redacted representation for diagnostics.
func (metadata Metadata) String() string { return "metadata_[redacted]" }

// GoString returns a redacted representation for Go-syntax diagnostics.
func (metadata Metadata) GoString() string { return metadata.String() }

// LogValue redacts metadata when passed directly to log/slog.
func (metadata Metadata) LogValue() slog.Value { return slog.StringValue(metadata.String()) }

func (metadata Metadata) equal(other Metadata) bool {
	return maps.Equal(metadata.values, other.values)
}

// ScopeKind distinguishes tenant-bound, system-wide, and deliberately
// unscoped work. The zero value is invalid.
type ScopeKind uint8

const (
	// ScopeTenant identifies work isolated to exactly one tenant.
	ScopeTenant ScopeKind = iota + 1
	// ScopeSystem identifies explicitly capable cross-tenant work.
	ScopeSystem
	// ScopeUnscoped identifies work that deliberately has no tenant semantics.
	ScopeUnscoped
)

// AdministrativeReason is auditable intent for exceptional operations.
// It is not an authorization decision.
type AdministrativeReason struct {
	actor     string
	purpose   string
	reference string
}

// NewAdministrativeReason validates explicit exceptional-operation intent.
func NewAdministrativeReason(actor, purpose, reference string) (AdministrativeReason, error) {
	if !validPrintable(actor, maxActorBytes, false) ||
		!validPrintable(purpose, maxPurposeBytes, false) ||
		!validPrintable(reference, maxReferenceBytes, true) {
		return AdministrativeReason{}, ErrInvalidAdministrativeReason
	}
	return AdministrativeReason{actor: actor, purpose: purpose, reference: reference}, nil
}

// Actor identifies the accountable administrative caller.
func (reason AdministrativeReason) Actor() string { return reason.actor }

// Purpose describes why exceptional scope is required.
func (reason AdministrativeReason) Purpose() string { return reason.purpose }

// Reference returns an optional external audit or change reference.
func (reason AdministrativeReason) Reference() string { return reason.reference }

// String returns a redacted representation for diagnostics.
func (reason AdministrativeReason) String() string { return "administrative_reason_[redacted]" }

// GoString returns a redacted representation for Go-syntax diagnostics.
func (reason AdministrativeReason) GoString() string { return reason.String() }

// LogValue redacts administrative intent when passed directly to log/slog.
func (reason AdministrativeReason) LogValue() slog.Value { return slog.StringValue(reason.String()) }

func (reason AdministrativeReason) valid() bool {
	return validPrintable(reason.actor, maxActorBytes, false) &&
		validPrintable(reason.purpose, maxPurposeBytes, false) &&
		validPrintable(reason.reference, maxReferenceBytes, true)
}

// SystemCapability makes system-wide intent visible in construction APIs. It
// does not grant permission; applications must authorize its creation.
type SystemCapability struct {
	reason AdministrativeReason
	valid  bool
}

// NewSystemCapability records the audited intent used by NewSystemScope.
func NewSystemCapability(reason AdministrativeReason) SystemCapability {
	return SystemCapability{reason: reason, valid: reason.valid()}
}

// String returns a redacted representation for diagnostics.
func (capability SystemCapability) String() string { return "system_capability_[redacted]" }

// GoString returns a redacted representation for Go-syntax diagnostics.
func (capability SystemCapability) GoString() string { return capability.String() }

// LogValue redacts the capability when passed directly to log/slog.
func (capability SystemCapability) LogValue() slog.Value {
	return slog.StringValue(capability.String())
}

// Scope is an immutable explicit operation scope. Its zero value is invalid.
type Scope struct {
	kind     ScopeKind
	tenant   TenantID
	metadata Metadata
	reason   AdministrativeReason
}

// NewTenantScope constructs work isolated to exactly id.
func NewTenantScope(id TenantID, metadata Metadata) (Scope, error) {
	if !id.Valid() {
		return Scope{}, ErrInvalidTenantID
	}
	return Scope{kind: ScopeTenant, tenant: id, metadata: cloneMetadata(metadata)}, nil
}

// NewSystemScope requires an explicit, valid system capability.
func NewSystemScope(capability SystemCapability, metadata Metadata) (Scope, error) {
	if !capability.valid || !capability.reason.valid() {
		return Scope{}, ErrCapabilityRequired
	}
	return Scope{kind: ScopeSystem, metadata: cloneMetadata(metadata), reason: capability.reason}, nil
}

// NewUnscopedScope constructs deliberately non-tenant work with an audit reason.
func NewUnscopedScope(reason AdministrativeReason, metadata Metadata) (Scope, error) {
	if !reason.valid() {
		return Scope{}, ErrInvalidAdministrativeReason
	}
	return Scope{kind: ScopeUnscoped, metadata: cloneMetadata(metadata), reason: reason}, nil
}

// Kind returns the explicit scope kind.
func (scope Scope) Kind() ScopeKind { return scope.kind }

// TenantID returns the tenant ID for tenant scope and the invalid zero value
// for every other scope kind.
func (scope Scope) TenantID() TenantID { return scope.tenant }

// Metadata returns an independently owned metadata value.
func (scope Scope) Metadata() Metadata { return cloneMetadata(scope.metadata) }

// AdministrativeReason returns exceptional-operation intent. Tenant scope
// returns the zero reason.
func (scope Scope) AdministrativeReason() AdministrativeReason { return scope.reason }

// Valid reports whether scope can be used at an enforcement boundary.
func (scope Scope) Valid() bool {
	switch scope.kind {
	case ScopeTenant:
		return scope.tenant.Valid() && !scope.reason.valid()
	case ScopeSystem, ScopeUnscoped:
		return !scope.tenant.Valid() && scope.reason.valid()
	default:
		return false
	}
}

// Equal compares all owned scope data.
func (scope Scope) Equal(other Scope) bool {
	return scope.kind == other.kind && scope.tenant.Equal(other.tenant) &&
		scope.reason == other.reason && scope.metadata.equal(other.metadata)
}

// String returns a redacted representation for diagnostics.
func (scope Scope) String() string {
	switch scope.kind {
	case ScopeTenant:
		return "tenant_scope_[redacted]"
	case ScopeSystem:
		return "system_scope_[redacted]"
	case ScopeUnscoped:
		return "unscoped_scope_[redacted]"
	default:
		return "invalid_scope_[redacted]"
	}
}

// GoString returns a redacted representation for Go-syntax diagnostics.
func (scope Scope) GoString() string { return scope.String() }

// LogValue redacts complete scope state when passed directly to log/slog.
func (scope Scope) LogValue() slog.Value { return slog.StringValue(scope.String()) }

func cloneMetadata(metadata Metadata) Metadata {
	return Metadata{values: maps.Clone(metadata.values)}
}

func validMetadataKey(value string) bool {
	if len(value) == 0 || len(value) > maxMetadataKey || !asciiAlphaNumeric(value[0]) {
		return false
	}
	for index := 1; index < len(value); index++ {
		char := value[index]
		if !asciiAlphaNumeric(char) && char != '-' && char != '_' && char != '.' {
			return false
		}
	}
	return true
}

func validPrintable(value string, maximum int, empty bool) bool {
	if len(value) > maximum || len(value) == 0 && !empty {
		return false
	}
	for index := range len(value) {
		if value[index] < 0x20 || value[index] > 0x7e {
			return false
		}
	}
	return true
}
