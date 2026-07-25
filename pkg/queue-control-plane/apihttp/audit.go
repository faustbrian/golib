package apihttp

import (
	"encoding/hex"
	"net/http"
	"net/url"
	"strconv"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/history"
)

const (
	defaultAuditPageSize uint32 = 100
	// MaxAuditPageSize bounds one public audit-history response.
	MaxAuditPageSize uint32 = 1_000
)

// AuditPage is one bounded tenant audit-history response.
type AuditPage struct {
	Entries      []AuditEntry `json:"entries"`
	NextSequence uint64       `json:"next_sequence,omitempty"`
}

// AuditEntry is the stable JSON representation of one chained audit event.
type AuditEntry struct {
	Sequence       uint64    `json:"sequence"`
	HashVersion    uint16    `json:"hash_version"`
	OccurredAt     time.Time `json:"occurred_at"`
	IdempotencyKey string    `json:"idempotency_key"`
	CommandID      string    `json:"command_id"`
	Actor          string    `json:"actor"`
	Action         string    `json:"action"`
	Target         string    `json:"target"`
	Result         string    `json:"result"`
	PreviousHash   string    `json:"previous_hash"`
	Hash           string    `json:"hash"`
}

func (h *handler) listAudit(writer http.ResponseWriter, request *http.Request) {
	principal, ok := authentication.PrincipalFromContext(request.Context())
	if !ok || principal.IsAnonymous() {
		writeProblem(writer, http.StatusUnauthorized, "unauthenticated")
		return
	}
	tenant := request.PathValue("tenant")
	if !validIdentity(tenant) {
		writeProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	if err := h.viewer.Authorize(
		request.Context(), tenant, principal.Subject(), controlplane.PermissionAuditView,
		controlplane.Target{Kind: controlplane.TargetWorkload, Name: "audit"},
	); err != nil {
		writeCommandError(writer, err)
		return
	}
	after, limit, err := parseAuditQuery(request.URL.Query())
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	stored, err := h.audit.ListTenant(request.Context(), tenant, after, limit)
	if err != nil {
		writeCommandError(writer, err)
		return
	}
	page := AuditPage{
		Entries:      make([]AuditEntry, len(stored.Entries)),
		NextSequence: stored.NextSequence,
	}
	for index, entry := range stored.Entries {
		page.Entries[index] = publicAuditEntry(entry)
	}
	writeJSON(writer, http.StatusOK, page)
}

func parseAuditQuery(values url.Values) (uint64, uint32, error) {
	for key, entries := range values {
		if (key != "after" && key != "limit") || len(entries) != 1 {
			return 0, 0, ErrInvalidConfiguration
		}
	}
	after := uint64(0)
	if raw, exists := values["after"]; exists {
		value, err := strconv.ParseUint(raw[0], 10, 64)
		if err != nil {
			return 0, 0, ErrInvalidConfiguration
		}
		after = value
	}
	limit := defaultAuditPageSize
	if raw, exists := values["limit"]; exists {
		value, err := strconv.ParseUint(raw[0], 10, 32)
		if err != nil || value == 0 || value > uint64(MaxAuditPageSize) {
			return 0, 0, ErrInvalidConfiguration
		}
		limit = uint32(value)
	}

	return after, limit, nil
}

func publicAuditEntry(entry history.Entry) AuditEntry {
	return AuditEntry{
		Sequence:       entry.Event.Sequence,
		HashVersion:    entry.Event.HashVersion,
		OccurredAt:     entry.Event.OccurredAt,
		IdempotencyKey: entry.Event.IdempotencyKey,
		CommandID:      entry.Event.CommandID,
		Actor:          entry.Event.Actor,
		Action:         entry.Event.Action,
		Target:         entry.Event.Target,
		Result:         entry.Event.Result,
		PreviousHash:   hex.EncodeToString(entry.PreviousHash[:]),
		Hash:           hex.EncodeToString(entry.Hash[:]),
	}
}
