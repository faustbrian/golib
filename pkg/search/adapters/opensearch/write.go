package opensearch

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	"github.com/faustbrian/golib/pkg/search"
)

// Write applies one externally versioned full-document mutation. Update and
// upsert are full-source replacements because OpenSearch's partial Update API
// does not provide the required external-version contract.
func (c *Client) Write(ctx context.Context, operation search.WriteOperation, refresh search.RefreshPolicy) (search.ItemOutcome, error) {
	if ctx == nil {
		return search.ItemOutcome{}, ErrContextRequired
	}
	if c.search == nil {
		return search.ItemOutcome{}, ErrSearchDisabled
	}
	validation := search.BulkRequest{Operations: []search.WriteOperation{operation}, Refresh: refresh}
	if err := validation.Validate(search.Capabilities{ExternalVersion: true, BulkPartialOutcomes: true}, c.search.Limits); err != nil {
		return search.ItemOutcome{}, err
	}
	target, err := c.search.Resolver.Resolve(ctx, operation.Tenant, operation.Index, IndexWrite)
	if err != nil {
		return search.ItemOutcome{}, ErrUnsafeIndexTarget
	}
	if !validIndexTarget(target) {
		return search.ItemOutcome{}, ErrUnsafeIndexTarget
	}

	method := http.MethodPut
	body := []byte(operation.Source)
	if operation.Action == search.ActionDelete {
		method, body = http.MethodDelete, nil
	}
	query := url.Values{
		"require_alias": []string{"true"},
		"version":       []string{strconv.FormatUint(operation.Version, 10)},
		"version_type":  []string{"external"},
	}
	switch refresh {
	case search.RefreshWaitFor:
		query.Set("refresh", "wait_for")
	case search.RefreshImmediate:
		query.Set("refresh", "true")
	}
	path := "/" + target.Name + "/_doc/" + url.PathEscape(operation.ID) + "?" + query.Encode()
	responseBody, status, err := c.executeWrite(ctx, method, path, body)
	if err != nil {
		return unknownWriteOutcome(operation), err
	}
	outcome, err := decodeWriteResponse(operation, status, responseBody)
	if err != nil {
		return unknownWriteOutcome(operation), &Failure{Operation: OperationWrite, Category: FailureMalformed, OutcomeKnown: false, cause: err}
	}
	if status >= 200 && status < 300 || status == http.StatusNotFound && operation.Action == search.ActionDelete {
		return outcome, nil
	}
	return outcome, responseFailure(OperationWrite, status, responseBody)
}

func (c *Client) executeWrite(ctx context.Context, method, path string, body []byte) (responseBytes []byte, status int, err error) {
	if ctx == nil {
		return nil, 0, ErrContextRequired
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, cancelledFailure(OperationWrite, err)
	}
	if err := c.begin(); err != nil {
		return nil, 0, err
	}
	requestCtx, cancel := context.WithTimeout(withOperation(ctx, OperationWrite), c.timeout)
	defer cancel()
	request, _ := http.NewRequestWithContext(requestCtx, method, path, bytes.NewReader(body))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, transportErr := c.client.Stream(request)
	if response == nil {
		return nil, 0, unknownTransportFailure(OperationWrite, transportErr)
	}
	defer func() { _ = response.Body.Close() }()
	if transportErr != nil {
		return nil, 0, unknownTransportFailure(OperationWrite, transportErr)
	}
	responseBody, err := readBounded(response.Body, c.maximumResponseBytes)
	if err != nil {
		return nil, response.StatusCode, &Failure{Operation: OperationWrite, Category: FailureMalformed, Status: response.StatusCode, OutcomeKnown: false, cause: err}
	}
	return responseBody, response.StatusCode, nil
}

func decodeWriteResponse(operation search.WriteOperation, status int, body []byte) (search.ItemOutcome, error) {
	var payload struct {
		ID      string `json:"_id"`
		Version uint64 `json:"_version"`
		Result  string `json:"result"`
		Error   *struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return search.ItemOutcome{}, ErrMalformedResponse
	}
	if payload.ID != "" && payload.ID != operation.ID {
		return search.ItemOutcome{}, ErrMalformedResponse
	}
	if status >= 200 && status < 300 &&
		(payload.ID != operation.ID || payload.Version != operation.Version || payload.Error != nil) {
		return search.ItemOutcome{}, ErrMalformedResponse
	}
	state, retryable := classifyWriteStatus(operation.Action, status, payload.Error)
	code := ""
	if payload.Error != nil {
		code = payload.Error.Type
		if !safeErrorCode(code) {
			code = "unknown"
		}
	}
	return search.ItemOutcome{Position: 0, ID: operation.ID, Action: operation.Action, State: state, Version: payload.Version, Code: code, Retryable: retryable}, nil
}

func classifyWriteStatus(action search.WriteAction, status int, failure *struct {
	Type string `json:"type"`
}) (search.OutcomeState, bool) {
	if status >= 200 && status < 300 {
		return search.OutcomeApplied, false
	}
	if status == http.StatusNotFound && action == search.ActionDelete {
		return search.OutcomeNotFound, false
	}
	if status == http.StatusConflict {
		return search.OutcomeVersionConflict, false
	}
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
		return search.OutcomeRejected, true
	}
	if failure != nil {
		return search.OutcomeFailed, false
	}
	return search.OutcomeUnknown, false
}

func unknownWriteOutcome(operation search.WriteOperation) search.ItemOutcome {
	return search.ItemOutcome{Position: 0, ID: operation.ID, Action: operation.Action, State: search.OutcomeUnknown, Retryable: true}
}
