package managementhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/faustbrian/golib/pkg/queue/management"
)

const maxCommandRequestBytes int64 = 16 << 10

type commandTarget struct {
	Kind management.TargetKind `json:"kind"`
	Name string                `json:"name"`
}

type commandSelection struct {
	Limit uint32 `json:"limit"`
}

type commandReplay struct {
	Destination       string                  `json:"destination"`
	IdempotencyPolicy management.ReplayPolicy `json:"idempotency_policy"`
}

type command struct {
	ID             string                   `json:"id"`
	IdempotencyKey string                   `json:"idempotency_key"`
	Actor          string                   `json:"actor"`
	Reason         string                   `json:"reason"`
	Protocol       protocolVersion          `json:"protocol"`
	Action         management.CommandAction `json:"action"`
	Target         commandTarget            `json:"target"`
	RequestedAt    time.Time                `json:"requested_at"`
	Deadline       time.Time                `json:"deadline"`
	Confirmed      bool                     `json:"confirmed"`
	Selection      *commandSelection        `json:"selection,omitempty"`
	Replay         *commandReplay           `json:"replay,omitempty"`
}

type commandResult struct {
	CommandID      string                         `json:"command_id"`
	IdempotencyKey string                         `json:"idempotency_key"`
	WorkerID       string                         `json:"worker_id"`
	Protocol       protocolVersion                `json:"protocol"`
	Status         management.CommandResultStatus `json:"status"`
	FailureCode    string                         `json:"failure_code"`
	CompletedAt    time.Time                      `json:"completed_at"`
}

// Execute sends one validated management command to the configured worker
// endpoint and returns its validated acknowledgement.
func (c *Client) Execute(
	ctx context.Context,
	request management.Command,
) (management.CommandResult, error) {
	if ctx == nil || request.Validate() != nil {
		return management.CommandResult{}, ErrInvalidRequest
	}
	wireRequest := transportCommand(request)
	body, err := json.Marshal(wireRequest)
	if err != nil || int64(len(body)) > maxCommandRequestBytes {
		return management.CommandResult{}, ErrInvalidRequest
	}
	endpoint := c.baseURL.JoinPath("v1", "commands")
	// #nosec G704 -- the operator-supplied base URL is validated at construction.
	httpRequest, err := http.NewRequestWithContext(
		ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body),
	)
	if err != nil {
		return management.CommandResult{}, ErrInvalidRequest
	}
	httpRequest.Header.Set("Authorization", "Bearer "+c.token)
	httpRequest.Header.Set("Content-Type", "application/json")
	// #nosec G704 -- management endpoints are explicit operator configuration.
	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		if ctx.Err() != nil {
			return management.CommandResult{}, ctx.Err()
		}

		return management.CommandResult{}, ErrRemoteFailure
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return management.CommandResult{}, ErrRemoteFailure
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, c.maxResponseBytes+1))
	if err != nil {
		return management.CommandResult{}, ErrRemoteFailure
	}
	if int64(len(data)) > c.maxResponseBytes {
		return management.CommandResult{}, ErrResponseTooLarge
	}
	var wireResult commandResult
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wireResult); err != nil || ensureEOF(decoder) != nil {
		return management.CommandResult{}, ErrInvalidResponse
	}
	result := managementCommandResult(wireResult)
	if result.Validate() != nil || !matchingResult(request, result) {
		return management.CommandResult{}, ErrInvalidResponse
	}

	return result, nil
}

func (h *handler) executeCommand(writer http.ResponseWriter, request *http.Request) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeProblem(writer, http.StatusUnsupportedMediaType, "unsupported_media_type")
		return
	}
	if request.ContentLength > maxCommandRequestBytes {
		writeProblem(writer, http.StatusRequestEntityTooLarge, "request_too_large")
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maxCommandRequestBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var wireCommand command
	if err := decoder.Decode(&wireCommand); err != nil {
		status := http.StatusBadRequest
		if maxBytesError(err) {
			status = http.StatusRequestEntityTooLarge
		}
		writeProblem(writer, status, commandProblemCode(status))
		return
	}
	if ensureEOF(decoder) != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	controlCommand := managementCommand(wireCommand)
	if controlCommand.Validate() != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	result, err := h.controller.Execute(request.Context(), controlCommand)
	if err != nil || result.Validate() != nil || !matchingResult(controlCommand, result) {
		writeProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(writer, transportCommandResult(result))
}

func transportCommand(value management.Command) command {
	result := command{
		ID: value.ID, IdempotencyKey: value.IdempotencyKey, Actor: value.Actor,
		Reason:   value.Reason,
		Protocol: protocolVersion{Major: value.Protocol.Major, Minor: value.Protocol.Minor},
		Action:   value.Action, Target: commandTarget{Kind: value.Target.Kind, Name: value.Target.Name},
		RequestedAt: value.RequestedAt, Deadline: value.Deadline, Confirmed: value.Confirmed,
	}
	if value.Selection != nil {
		result.Selection = &commandSelection{Limit: value.Selection.Limit}
	}
	if value.Replay != nil {
		result.Replay = &commandReplay{
			Destination:       value.Replay.Destination,
			IdempotencyPolicy: value.Replay.IdempotencyPolicy,
		}
	}

	return result
}

func managementCommand(value command) management.Command {
	result := management.Command{
		ID: value.ID, IdempotencyKey: value.IdempotencyKey, Actor: value.Actor,
		Reason:      value.Reason,
		Protocol:    management.ProtocolVersion{Major: value.Protocol.Major, Minor: value.Protocol.Minor},
		Action:      value.Action,
		Target:      management.Target{Kind: value.Target.Kind, Name: value.Target.Name},
		RequestedAt: value.RequestedAt, Deadline: value.Deadline, Confirmed: value.Confirmed,
	}
	if value.Selection != nil {
		result.Selection = &management.Selection{Limit: value.Selection.Limit}
	}
	if value.Replay != nil {
		result.Replay = &management.ReplayOptions{
			Destination:       value.Replay.Destination,
			IdempotencyPolicy: value.Replay.IdempotencyPolicy,
		}
	}

	return result
}

func transportCommandResult(value management.CommandResult) commandResult {
	return commandResult{
		CommandID: value.CommandID, IdempotencyKey: value.IdempotencyKey,
		WorkerID: value.WorkerID,
		Protocol: protocolVersion{Major: value.Protocol.Major, Minor: value.Protocol.Minor},
		Status:   value.Status, FailureCode: value.FailureCode, CompletedAt: value.CompletedAt,
	}
}

func managementCommandResult(value commandResult) management.CommandResult {
	return management.CommandResult{
		CommandID: value.CommandID, IdempotencyKey: value.IdempotencyKey,
		WorkerID: value.WorkerID,
		Protocol: management.ProtocolVersion{Major: value.Protocol.Major, Minor: value.Protocol.Minor},
		Status:   value.Status, FailureCode: value.FailureCode, CompletedAt: value.CompletedAt,
	}
}

func matchingResult(command management.Command, result management.CommandResult) bool {
	return command.ID == result.CommandID && command.IdempotencyKey == result.IdempotencyKey
}

func maxBytesError(err error) bool {
	var target *http.MaxBytesError
	return errors.As(err, &target)
}

func commandProblemCode(status int) string {
	if status == http.StatusRequestEntityTooLarge {
		return "request_too_large"
	}

	return "invalid_request"
}

var _ management.Controller = (*Client)(nil)
