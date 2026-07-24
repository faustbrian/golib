package apihttp

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	controlkubernetes "github.com/faustbrian/golib/pkg/queue-control-plane/kubernetes"
)

const defaultWorkloadPageSize int64 = 100

type workloadQuery struct {
	limit         int64
	continueToken string
}

func (h *handler) listWorkloads(writer http.ResponseWriter, request *http.Request) {
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
		request.Context(),
		tenant,
		principal.Subject(),
		controlplane.PermissionView,
		controlplane.Target{Kind: controlplane.TargetWorkload, Name: "kubernetes"},
	); err != nil {
		writeCommandError(writer, err)

		return
	}

	query, err := parseWorkloadQuery(request.URL.Query())
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_request")

		return
	}
	page, err := h.workloads.ListTenantWorkloads(
		request.Context(),
		tenant,
		query.limit,
		query.continueToken,
	)
	if err != nil {
		writeCommandError(writer, err)

		return
	}

	writeJSON(writer, http.StatusOK, page)
}

func parseWorkloadQuery(values url.Values) (workloadQuery, error) {
	query := workloadQuery{limit: defaultWorkloadPageSize}
	allowed := map[string]bool{"limit": true, "continue": true}
	for key, entries := range values {
		if !allowed[key] || len(entries) != 1 {
			return workloadQuery{}, ErrInvalidConfiguration
		}
	}

	if raw := values.Get("limit"); raw != "" {
		limit, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || limit < 1 || limit > controlkubernetes.MaxPageSize {
			return workloadQuery{}, ErrInvalidConfiguration
		}
		query.limit = limit
	}
	query.continueToken = values.Get("continue")
	if _, exists := values["continue"]; exists &&
		(strings.TrimSpace(query.continueToken) == "" ||
			len(query.continueToken) > controlkubernetes.MaxContinueTokenBytes) {
		return workloadQuery{}, ErrInvalidConfiguration
	}

	return query, nil
}
