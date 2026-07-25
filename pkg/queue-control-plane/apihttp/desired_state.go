package apihttp

import (
	"errors"
	"net/http"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/control"
	controlpostgres "github.com/faustbrian/golib/pkg/queue-control-plane/postgres"
	queue "github.com/faustbrian/golib/pkg/queue/management"
)

func (h *handler) getDesiredState(writer http.ResponseWriter, request *http.Request) {
	principal, ok := authentication.PrincipalFromContext(request.Context())
	if !ok || principal.IsAnonymous() {
		writeProblem(writer, http.StatusUnauthorized, "unauthenticated")
		return
	}
	tenant := request.PathValue("tenant")
	target := controlplane.Target{
		Kind: controlplane.TargetKind(request.PathValue("kind")),
		Name: request.PathValue("name"),
	}
	if !validIdentity(tenant) || !validDesiredTarget(target) || len(request.URL.Query()) != 0 {
		writeProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	if err := h.viewer.Authorize(
		request.Context(), tenant, principal.Subject(),
		controlplane.PermissionView, target,
	); err != nil {
		writeCommandError(writer, err)
		return
	}
	record, err := h.desiredState.Get(request.Context(), tenant, target)
	if errors.Is(err, controlpostgres.ErrDesiredStateNotFound) {
		writeProblem(writer, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		writeProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	result := desiredStateModel(record)
	if result.Validate() != nil {
		writeProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}

	writeJSON(writer, http.StatusOK, result)
}

func validDesiredTarget(target controlplane.Target) bool {
	if !validIdentity(target.Name) {
		return false
	}
	switch target.Kind {
	case controlplane.TargetQueue,
		controlplane.TargetWorker,
		controlplane.TargetWorkerGroup:
		return true
	default:
		return false
	}
}

func desiredStateModel(record control.DesiredRecord) queue.DesiredRecord {
	return queue.DesiredRecord{
		Target: queue.Target{
			Kind: queue.TargetKind(record.Target.Kind),
			Name: record.Target.Name,
		},
		State: queue.DesiredState(record.State), Revision: record.Revision,
		ChangedAt: record.ChangedAt.UTC(), CommandID: record.CommandKey,
	}
}
