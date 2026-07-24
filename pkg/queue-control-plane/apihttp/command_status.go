package apihttp

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	controlpostgres "github.com/faustbrian/golib/pkg/queue-control-plane/postgres"
)

const (
	defaultCommandPageSize uint32 = 100
	// MaxCommandPageSize bounds one public command-history response.
	MaxCommandPageSize = controlpostgres.MaxCommandPageSize
	// MaxCommandCursorBytes bounds public opaque command-history cursors.
	MaxCommandCursorBytes = controlpostgres.MaxCommandCursorBytes
)

// CommandHistoryPage is one bounded tenant command-history response.
type CommandHistoryPage struct {
	Commands   []CommandHistoryEntry `json:"commands"`
	NextCursor string                `json:"next_cursor,omitempty"`
}

// CommandHistoryEntry is one immutable command envelope and durable outcome.
type CommandHistoryEntry struct {
	CommandID            string                     `json:"command_id"`
	IdempotencyKey       string                     `json:"idempotency_key"`
	Actor                string                     `json:"actor"`
	AuthenticationMethod string                     `json:"authentication_method"`
	Reason               string                     `json:"reason"`
	Action               controlplane.Action        `json:"action"`
	Capability           string                     `json:"capability"`
	Target               TargetRequest              `json:"target"`
	RequestedAt          time.Time                  `json:"requested_at"`
	Deadline             time.Time                  `json:"deadline"`
	Confirmed            bool                       `json:"confirmed"`
	Selection            *SelectionRequest          `json:"selection,omitempty"`
	Replay               *ReplayRequest             `json:"replay,omitempty"`
	Scale                *ScaleRequest              `json:"scale,omitempty"`
	Result               controlplane.CommandResult `json:"result"`
}

func (h *handler) listCommandHistory(writer http.ResponseWriter, request *http.Request) {
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
		request.Context(), tenant, principal.Subject(), controlplane.PermissionView,
		controlplane.Target{Kind: controlplane.TargetWorkload, Name: "commands"},
	); err != nil {
		writeCommandError(writer, err)
		return
	}
	cursor, limit, err := parseCommandQuery(request.URL.Query())
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	stored, err := h.commandHistory.ListTenant(request.Context(), tenant, cursor, limit)
	if err != nil {
		writeCommandError(writer, err)
		return
	}
	page := CommandHistoryPage{
		Commands:   make([]CommandHistoryEntry, len(stored.Records)),
		NextCursor: stored.NextCursor,
	}
	for index, record := range stored.Records {
		page.Commands[index] = publicCommandRecord(record)
	}
	writeJSON(writer, http.StatusOK, page)
}

func (h *handler) getCommandResult(writer http.ResponseWriter, request *http.Request) {
	principal, ok := authentication.PrincipalFromContext(request.Context())
	if !ok || principal.IsAnonymous() {
		writeProblem(writer, http.StatusUnauthorized, "unauthenticated")
		return
	}
	tenant := request.PathValue("tenant")
	key := request.PathValue("key")
	if !validIdentity(tenant) || !validIdentity(key) {
		writeProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	if err := h.viewer.Authorize(
		request.Context(), tenant, principal.Subject(), controlplane.PermissionView,
		controlplane.Target{Kind: controlplane.TargetWorkload, Name: "commands"},
	); err != nil {
		writeCommandError(writer, err)
		return
	}
	result, err := h.commandResults.Get(request.Context(), tenant, key)
	if err != nil {
		writeCommandError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func parseCommandQuery(values url.Values) (string, uint32, error) {
	for key, entries := range values {
		if (key != "cursor" && key != "limit") || len(entries) != 1 {
			return "", 0, ErrInvalidConfiguration
		}
	}
	cursor := values.Get("cursor")
	if len(cursor) > MaxCommandCursorBytes {
		return "", 0, ErrInvalidConfiguration
	}
	limit := defaultCommandPageSize
	if raw, exists := values["limit"]; exists {
		value, err := strconv.ParseUint(raw[0], 10, 32)
		if err != nil || value == 0 || value > uint64(MaxCommandPageSize) {
			return "", 0, ErrInvalidConfiguration
		}
		limit = uint32(value)
	}

	return cursor, limit, nil
}

func publicCommandRecord(record controlpostgres.CommandRecord) CommandHistoryEntry {
	command := record.Command
	entry := CommandHistoryEntry{
		CommandID:            command.CommandID,
		IdempotencyKey:       command.IdempotencyKey,
		Actor:                command.Actor,
		AuthenticationMethod: command.AuthenticationMethod,
		Reason:               command.Reason,
		Action:               command.Action,
		Capability:           command.Capability,
		Target:               TargetRequest{Kind: command.Target.Kind, Name: command.Target.Name},
		RequestedAt:          command.RequestedAt,
		Deadline:             command.Deadline,
		Confirmed:            command.Confirmed,
		Result:               record.Result,
	}
	if command.Selection != nil {
		entry.Selection = &SelectionRequest{Limit: command.Selection.Limit}
	}
	if command.Replay != nil {
		entry.Replay = &ReplayRequest{
			Destination:       command.Replay.Destination,
			IdempotencyPolicy: command.Replay.IdempotencyPolicy,
		}
	}
	if command.Scale != nil {
		entry.Scale = &ScaleRequest{Replicas: command.Scale.Replicas}
	}

	return entry
}
